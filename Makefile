.PHONY: all build run

.ONESHELL:
.SHELLFLAGS = -ec

SHELL = /bin/bash

.DEFAULT_GOAL := all

all: run

build:
	env GOOS=darwin GOARCH=arm64 go build -o bin/cloudmc_usage_trends-osx_m1
	env GOOS=linux GOARCH=amd64 go build -o bin/cloudmc_usage_trends-linux_amd64
	env GOOS=windows GOARCH=amd64 go build -o bin/cloudmc_usage_trends-windows_amd64

run:
	go build
	./cloudmc_usage_trends