// Listens for consul events via watches and updates quota usage in consul.
// @author Lenko Donchev

package main

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"

	"fmt"

	"github.com/hashicorp/nomad/api"

	consul "github.com/hashicorp/consul/api"

	"github.com/jet/nomad-service-alerter/logger"

	"./utils"
)

const (
	ERR_UNEXPECTED_QUOTA_KEY = 1
	ERR_JOB_LIST_NOMAD       = 2
	ERR_JOB_INFO_NOMAD       = 3
	ERR_FAILED_TO_SAVE_KEY   = 4
	ERR_FAILED_TO_DELETE_KEY = 5
	ERR_CONFIG_FILE          = 6
	ERR_NOMAD_CLIENT_CREATE  = 7
	ERR_CONSUL_CLIENT        = 8
)

func main() {
	logger.Init(ioutil.Discard, os.Stdout, os.Stdout, os.Stderr)
	logger.Info.Printf("Starting... \n")

	viper.SetConfigName("cs") // name of config file (without extension)
	viper.AddConfigPath("$HOME/.cs")
	err := viper.ReadInConfig()
	if err != nil {
		logger.Error.Printf("Unable to read the config file. Err: %s \n", err)
		os.Exit(ERR_CONFIG_FILE)
	}

	consulHost := utils.GetConfigString("consul_server")
	datacenter := utils.GetConfigString("consul_datacenter")
	host := utils.GetConfigString("nomad_server")

	config := consul.DefaultConfig()
	config.Address = consulHost
	config.Datacenter = datacenter
	config.Token = ""
	consulClient, err := consul.NewClient(config)
	if err != nil {
		logger.Error.Printf("Unable to create consul client. Host:%s, err: %s  \n", host, err)
		os.Exit(ERR_CONSUL_CLIENT)
	}

	update_quota_usage(host, consulClient)
}

func update_quota_usage(host string, consulClient *consul.Client) {
	logger.Info.Printf("Connecting to Nomad and getting jobs... \n")

	client, cerr := api.NewClient(&api.Config{Address: host, TLSConfig: &api.TLSConfig{}})
	if cerr != nil {
		logger.Error.Printf("Unable to create nomad client. Host: %s, err: %s  \n", host, cerr)
		os.Exit(ERR_NOMAD_CLIENT_CREATE)
	}

	optsNomad := &api.QueryOptions{}

	jobList, _, err := client.Jobs().List(optsNomad)
	if err != nil {
		logger.Error.Printf("Cannot get job List from Nomad : %v \n", err.Error())
		os.Exit(ERR_JOB_LIST_NOMAD)
	}

	quota_usage_map := make(map[string]int)
	resetQuotaUsage(consulClient)

	for _, job := range jobList {
		logger.Info.Printf("processing job id=%s \n", job.ID)

		value, _, err := client.Jobs().Info(job.ID, optsNomad)
		if err != nil {
			logger.Error.Printf("Cannot get job info from Nomad : %v \n", err.Error())
			os.Exit(ERR_JOB_INFO_NOMAD)
		}

		if *value.Status != "running" && *value.Status != "pending" {
			logger.Info.Printf("Excluding job id=%s from quota calculations. Job status=%s \n", job.ID, *value.Status)
			continue
		}

		for i := 0; i < len(value.TaskGroups); i++ {
			for j := 0; j < len(value.TaskGroups[i].Tasks); j++ {
				quota_key := "cpu"
				calculateQuotaUsage(quota_key, utils.BuildNomadQuotaKey(quota_key, value.Constraints), *value.TaskGroups[i].Tasks[j].Resources.CPU, &quota_usage_map)

				quota_key = "memory"
				calculateQuotaUsage(quota_key, utils.BuildNomadQuotaKey(quota_key, value.Constraints), *value.TaskGroups[i].Tasks[j].Resources.MemoryMB, &quota_usage_map)
			}
		}
	}

	updateQuotaUsage(&quota_usage_map, consulClient)
}

func calculateQuotaUsage(quota_key string, quota_usage_key string, quota_usage_value int, quota_usage_map *map[string]int) {
	usage_map := *quota_usage_map

	quotaUsageValue := usage_map[quota_usage_key] + quota_usage_value
	logger.Info.Printf("Updating quota usage map with key:%s. And value:%d \n", quota_usage_key, quotaUsageValue)
	usage_map[quota_usage_key] = quotaUsageValue
}

func updateQuotaUsage(quota_usage_map *map[string]int, consulClient *consul.Client) {
	logger.Info.Printf("Updating quota usage... \n")

	usage_map := *quota_usage_map

	for k, v := range usage_map {
		if strings.Contains(k, "---") { // skipping keys that dont have all the needed info
			continue
		}

		logger.Info.Printf("key[%s] value[%s]\n", k, strconv.Itoa(v))

		_, err := consulClient.KV().Put(&consul.KVPair{
			Key:   fmt.Sprintf("quotas/usage/%s", k),
			Value: []byte(strconv.Itoa(v))}, nil)
		if err != nil {
			logger.Error.Printf("Failed to save key[%s] value[%s],  err:%s\n", k, strconv.Itoa(v), err)
			os.Exit(ERR_FAILED_TO_SAVE_KEY)
		}
	}
}

func resetQuotaUsage(consulClient *consul.Client) {
	logger.Info.Printf("Resetting quota usage... \n")

	key := "quotas/usage"

	if _, err := consulClient.KV().DeleteTree(key, nil); err != nil {
		logger.Error.Printf("Error! Did not delete key %s: %s", key, err)

		os.Exit(ERR_FAILED_TO_DELETE_KEY)
	}
}
