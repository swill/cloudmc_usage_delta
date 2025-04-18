package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v9"
	"github.com/nlopes/slack"
	"github.com/spf13/viper"
	"github.com/swill/cmc_core"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var (
	slackAPI *slack.Client
)

var query string = `{
	"size": 0,
	"query": {
		"bool": {
			"must": [],
			"filter": [
				{
					"term": {
						"organizationId": "{{.OrgID}}"
					}
				},
				{
					"term": {
						"reprocessing": false
					}
				},
				{
					"term": {
						"isNative": true
					}
				},                    
				{
					"range": {
						"startDate": {
							"format": "strict_date_optional_time",
							"gt": "{{.StartDate}}",
							"lte": "{{.EndDate}}"
						}
					}
				}
			],
			"should": [],
			"must_not": []
		}
	},
	"aggs": {
		"organization": {
			"terms": {
				"field": "organizationId",
				"size": 10000
			},
			"aggs": {
				"connection": {
					"terms": {
						"field": "connectionId",
						"size": 1000
					},
					"aggs": {
						"daily": {
							"date_histogram": {
								"field": "startDate",
								"calendar_interval": "day"
							},
							"aggs": {
								"totalUsage": {
									"sum": {
										"field": "nativeBillingCost"
									}
								}
							}
						}
					}
				}
			}
		}
	}
}`

type QueryOps struct {
	OrgID     string
	StartDate string
	EndDate   string
}

type QueryResult struct {
	Took     int  `json:"took"`
	TimedOut bool `json:"timed_out"`
	Shards   struct {
		Total      int `json:"total"`
		Successful int `json:"successful"`
		Skipped    int `json:"skipped"`
		Failed     int `json:"failed"`
	} `json:"_shards"`
	Hits struct {
		Total struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		} `json:"total"`
		MaxScore any   `json:"max_score"`
		Hits     []any `json:"hits"`
	} `json:"hits"`
	Aggregations struct {
		Organization struct {
			DocCountErrorUpperBound int `json:"doc_count_error_upper_bound"`
			SumOtherDocCount        int `json:"sum_other_doc_count"`
			Buckets                 []struct {
				Key        string `json:"key"`
				DocCount   int    `json:"doc_count"`
				Connection struct {
					DocCountErrorUpperBound int `json:"doc_count_error_upper_bound"`
					SumOtherDocCount        int `json:"sum_other_doc_count"`
					Buckets                 []struct {
						Key      string `json:"key"`
						DocCount int    `json:"doc_count"`
						Daily    struct {
							Buckets []struct {
								KeyAsString string `json:"key_as_string"`
								Key         int64  `json:"key"`
								DocCount    int    `json:"doc_count"`
								TotalUsage  struct {
									Value float64 `json:"value"`
								} `json:"totalUsage"`
							} `json:"buckets"`
						} `json:"daily"`
					} `json:"buckets"`
				} `json:"connection"`
			} `json:"buckets"`
		} `json:"organization"`
	} `json:"aggregations"`
}

func main() {
	// read config from a file
	viper.SetConfigName("cloudmc_usage_trends")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Error parsing config file: %s", err)
	}

	// set global defaults (if needed)
	viper.SetDefault("QUERY_DAYS_AGO", 2)
	viper.SetDefault("THRESHOLD", 0.05)

	// validate we have all the config required to start
	missingConfig := false
	if !viper.IsSet("CMC_ENDPOINT") || viper.GetString("CMC_ENDPOINT") == "" {
		log.Println("Error: Missing required 'CMC_ENDPOINT' variable.")
		missingConfig = true
	}
	if !viper.IsSet("CMC_KEY") || viper.GetString("CMC_KEY") == "" {
		log.Println("Error: Missing required 'CMC_KEY' variable.")
		missingConfig = true
	}
	if !viper.IsSet("ELASTIC_CLOUDID") || viper.GetString("ELASTIC_CLOUDID") == "" {
		log.Println("Error: Missing required 'ELASTIC_CLOUDID' variable.")
		missingConfig = true
	}
	if !viper.IsSet("ELASTIC_KEY") || viper.GetString("ELASTIC_KEY") == "" {
		log.Println("Error: Missing required 'ELASTIC_KEY' variable.")
		missingConfig = true
	}
	if ((!viper.IsSet("SLACK_TOKEN") || viper.GetString("SLACK_TOKEN") == "") && viper.IsSet("SLACK_CHANNEL") && viper.GetString("SLACK_CHANNEL") != "") ||
		((!viper.IsSet("SLACK_CHANNEL") || viper.GetString("SLACK_CHANNEL") == "") && viper.IsSet("SLACK_TOKEN") && viper.GetString("SLACK_TOKEN") != "") {
		log.Printf("Error: Both 'SLACK_TOKEN' and 'SLACK_CHANNEL' must be defined in the config file to enable the slack integration.")
		missingConfig = true
	}
	if missingConfig {
		log.Fatal("Missing required configuration details, please update the config file.")
	}

	// setup the time boundaries
	dateFormat := "2006-01-02"
	today := time.Now().Truncate(24 * time.Hour)
	startDate := today.AddDate(0, 0, -(viper.GetInt("QUERY_DAYS_AGO") + 2)).Format(dateFormat) // two days before the end date
	endDate := today.AddDate(0, 0, -(viper.GetInt("QUERY_DAYS_AGO"))).Format(dateFormat)

	// setup the title case helper
	title := cases.Title(language.English)
	// setup the currency printer
	cp := message.NewPrinter(message.MatchLanguage("en"))

	// setup slack if the config exists
	if viper.IsSet("SLACK_TOKEN") && viper.GetString("SLACK_TOKEN") != "" && viper.IsSet("SLACK_CHANNEL") && viper.GetString("SLACK_CHANNEL") != "" {
		slackAPI = slack.New(viper.GetString("SLACK_TOKEN"))
	}

	// define the query template
	tpl, err := template.New("query").Parse(query)
	if err != nil {
		log.Fatal("Error parsing query template: ", err)
	}

	// setup the Elastic API endpoint
	es_client, err := elasticsearch.NewClient(elasticsearch.Config{
		CloudID: viper.GetString("ELASTIC_CLOUDID"),
		APIKey:  viper.GetString("ELASTIC_KEY"),
	})
	if err != nil {
		log.Fatal("Error setting up Elastic client: ", err)
	}

	// setup the CloudMC API endpoint context
	ctx := context.WithValue(
		context.Background(),
		cmc_core.ContextAPIKeys,
		map[string]cmc_core.APIKey{
			"ApiKeyAuth": {Key: viper.GetString("CMC_KEY")},
		},
	)
	cfg := cmc_core.NewConfiguration()
	cfg.Servers = cmc_core.ServerConfigurations{
		{URL: viper.GetString("CMC_ENDPOINT")},
	}

	// create a new CloudMC client
	client := cmc_core.NewAPIClient(cfg)

	// get the organizations
	orgs, err := getOrganizations(client, ctx)
	if err != nil {
		log.Fatal("Error getting organizations: ", err)
	}

	// loop through the orgs and query for usage
	for _, org := range orgs.GetData() {
		// query data
		data := QueryOps{
			OrgID:     *org.Id,
			StartDate: startDate,
			EndDate:   endDate,
		}

		// inject query data into query template
		var buf bytes.Buffer
		err = tpl.Execute(&buf, data)
		if err != nil {
			log.Fatal("Error executing query template: ", err)
		}

		resp, err := es_client.Search(
			es_client.Search.WithBody(strings.NewReader(buf.String())),
		)
		if err != nil {
			log.Fatal("Error executing the elastic search: ", err)
		}

		// populate the query result object
		var result QueryResult
		err = json.NewDecoder(resp.Body).Decode(&result)
		if err != nil {
			log.Fatal("Error decoding query results: ", err)
		}

		// calculate the output
		for _, orgAgg := range result.Aggregations.Organization.Buckets {
			for _, scAgg := range orgAgg.Connection.Buckets {
				if len(scAgg.Daily.Buckets) > 1 {
					dif := scAgg.Daily.Buckets[1].TotalUsage.Value - scAgg.Daily.Buckets[0].TotalUsage.Value
					denominator := scAgg.Daily.Buckets[0].TotalUsage.Value
					if dif < 0 {
						denominator = scAgg.Daily.Buckets[1].TotalUsage.Value
					}
					if math.Abs(dif/denominator) > viper.GetFloat64("THRESHOLD") {
						cmcSC, err := getServiceConnection(scAgg.Key, client, ctx)
						if err != nil {
							log.Fatal("Error getting service connection: ", err)
						}
						scData := cmcSC.GetData()

						dayOne, _ := time.Parse("20060102", scAgg.Daily.Buckets[0].KeyAsString[:8])
						dayTwo, _ := time.Parse("20060102", scAgg.Daily.Buckets[1].KeyAsString[:8])

						verb := "increased"
						if dif < 0 {
							verb = "decreased"
						}
						delta := math.Abs(dif/denominator) * 100

						output := fmt.Sprintf("Daily usage %s by %.1f%% for '%s' in %s (%s) between %s and %s, from $%s to $%s",
							verb, delta, *org.Name, title.String(*scData.Type), *scData.Name, dayOne.Format(dateFormat), dayTwo.Format(dateFormat),
							cp.Sprintf("%.2f", scAgg.Daily.Buckets[0].TotalUsage.Value),
							cp.Sprintf("%.2f", scAgg.Daily.Buckets[1].TotalUsage.Value))

						if slackAPI != nil {
							opt := slack.MsgOptionText(output, false)
							slackAPI.PostMessage(viper.GetString("SLACK_CHANNEL"), opt)
						} else {
							log.Println(output)
						}
					}
				}
			}
		}
	}
}

func getOrganizations(client *cmc_core.APIClient, ctx context.Context) (*cmc_core.FindAllOrganization200Response, error) {
	res, _, err := client.OrganizationAPI.FindAllOrganization(ctx).Execute()
	if err != nil {
		return nil, err
	}
	return res, nil
}

func getServiceConnection(sc string, client *cmc_core.APIClient, ctx context.Context) (*cmc_core.FindServiceConnections200Response, error) {
	res, _, err := client.ServiceConnectionAPI.FindServiceConnections(ctx, sc).Execute()
	if err != nil {
		return nil, err
	}
	return res, nil
}
