CloudMC Usage Trends
--------------------

A script which queries the CloudMC API and Elastic to pull out the `daily usage` for every CSP customer to validate if there is a change beyond a threshold.

## SETUP

1. Run `cd cloudmc_usage_trends`
2. Run `cp cloudmc_usage_trends.toml.sample cloudmc_usage_trends.toml`
3. Navigate to `Profile > API Credentials > [Generate API Key]` in CloudMC to get your API key
    - Populate `CMC_ENDPOINT` and `CMC_KEY` in `cloudmc_usage_trends.toml` appropriately
4. Navigate to your Elastic deployment to get your `Cloud ID` and to generate a read-only API key
    - Populate `ELASTIC_CLOUDID` and `ELASTIC_KEY` in `cloudmc_usage_trends.toml` appropriately
5. Generate the CloudMC SDK using the `Makefile` in the following project
    - https://github.com/swill/cloudops_golang_sdk_generator
6. Update the `replace` configuration in `go.mod` to target your generated SDK

## USAGE

A `Makefile` has been provided to simplify building and running.

To run the script.

```bash
$ make
```

To build cross platform binaries.

```bash
$ make build
```

## AUTOMATED RUN

I have included a `plist` file to enable the automatic running of the script on a Mac on each weekday.

To configure the script to run daily, do the following.

1. Run `cd cloudmc_usage_trends`
2. Run `cp local.cloudmc_usage_trends.plist.sample local.cloudmc_usage_trends.plist`
    - Update each reference to the path `/absolute/path/to/` to the location of this repository
3. Run `cp local.cloudmc_usage_trends.plist ~/Library/LaunchAgents`
4. Run `launchctl load ~/Library/LaunchAgents/local.cloudmc_usage_trends.plist`


If you wish to turn off the daily running of the script, do the following.
```bash
launchctl unload ~/Library/LaunchAgents/local.cloudmc_usage_trends.plist
```

## CONFIGURATION

The following configurations are available.

```toml
### CloudMC Credentials
CMC_ENDPOINT="" # CloudMC API Endpoint
CMC_KEY=""      # CloudMC API Key


### Elastic Credentials
ELASTIC_CLOUDID="" # Elastic Cloud ID
ELASTIC_KEY=""     # Elastic API Key

### Optional: Slack Credentials & Channel
# SLACK_TOKEN=""
# SLACK_CHANNEL=""

### DEFAULTS
# QUERY_DAYS_AGO=2
# THRESHOLD=0.05
```