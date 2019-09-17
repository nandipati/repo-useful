// General Utilities
// @author Lenko Donchev

package utils

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elbv2"
	consulAPI "github.com/hashicorp/consul/api"
	nomadapi "github.com/hashicorp/nomad/api"
	vaultAPI "github.com/hashicorp/vault/api"

	"github.com/spf13/viper"
)

type ServiceAddress struct {
	Host      string
	Port      int
	Node      string
	LastIndex uint64
}

type AWSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	SecurityToken   string
}

const (
	CONSUL_INFRASTRUCTURE_PATH            = "infrastructure/"
	VAULT_INFRASTRUCTURE_PATH             = "secret/data/infrastructure/"
	ERR_NOT_FOUND                         = 7
	VAULT_DATA_KEY                        = "value"
	ERR_CONSUL_CLIENT                     = 8
	ERR_VAULT_READ                        = 9
	ERR_VAULT_CANNOT_WRITE                = 7
	ERR_CANNOT_CONNECT_TO_VAULT           = 3
	VAULT_ACCESS_TOKEN_KEY_NAME_IN_CONSUL = "vault_token"
	NOMAD_GROUP_CONSTRAINT                = "${meta.group}"
	NOMAD_ENV_CONSTRAINT                  = "${meta.env}"
	NOMAD_QUOTA_KEY_SEPARATOR             = "--"
	EXIT_SUCCESS                          = 0
	ERR_CERT_UPLOAD                       = 10
)

func GetConfigString(config_key string) string {
	active_config_profile := viper.GetString("active")

	return viper.GetString(active_config_profile + "." + config_key)
}

func GetVaultClient() vaultAPI.Client {
	vaultCFG := vaultAPI.DefaultConfig()
	vaultCFG.Address = GetConfigString("vault_address")

	var err error
	vClient, err := vaultAPI.NewClient(vaultCFG)
	if err != nil {
		log.Fatal(err)
		os.Exit(13)
	}

	vClient.SetToken(GetDataFromConsul(VAULT_ACCESS_TOKEN_KEY_NAME_IN_CONSUL))

	return *vClient
}

func GetConsulClient() *consulAPI.Client {
	consulAddress := GetConfigString("consul_server")
	datacenter := GetConfigString("consul_datacenter")

	config := consulAPI.DefaultConfig()
	config.Address = consulAddress
	config.Datacenter = datacenter
	config.Token = ""
	consulClient, err := consulAPI.NewClient(config)
	if err != nil {
		fmt.Printf("Unable to create client(%v): %v", consulAddress, err)
		os.Exit(1)
	}

	return consulClient
}

func GetDataFromConsul(dataName string) string {
	client := GetConsulClient()
	kvp, _, err := client.KV().Get(CONSUL_INFRASTRUCTURE_PATH+dataName, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(ERR_CONSUL_CLIENT)
	}

	if kvp == nil {
		fmt.Printf("unable to find data in consul for dataName=%s, path=%s \n", dataName, CONSUL_INFRASTRUCTURE_PATH)
		os.Exit(ERR_NOT_FOUND)
	}

	return string(kvp.Value)
}

func SaveDataInVault(dataName string, dataValue string) {
	vault := GetVaultClient()

	pathArg := VAULT_INFRASTRUCTURE_PATH

	dataValueMap := map[string]interface{}{
		dataName: dataValue,
	}

	dataMap := map[string]interface{}{
		"data": dataValueMap,
	}

	_, err := vault.Logical().Write(pathArg, dataMap)
	if err != nil {
		fmt.Printf("unable to store data(%s) in vault , exiting. Error: %v. Data:%v  \n", dataName, err, dataMap)
		os.Exit(ERR_VAULT_CANNOT_WRITE)
	}
}

func GetDataFromVault(dataName string) string {
	vault := GetVaultClient()

	pathArg := VAULT_INFRASTRUCTURE_PATH

	secret, err := vault.Logical().Read(pathArg)
	if err != nil {
		fmt.Printf("error reading path %s. Error: %s  \n", pathArg, err)
		os.Exit(ERR_VAULT_READ)
	}
	if secret == nil {
		fmt.Printf("no value found at %s \n", pathArg)
		os.Exit(ERR_VAULT_READ)
	}
	if secret.Data == nil {
		fmt.Printf("\"data\" not found in wrapping response, secret=%v", secret)
		os.Exit(ERR_VAULT_READ)
	}

	_, ok := secret.Data["data"]
	if !ok {
		fmt.Printf("\"data\" not found in wrapping response secret=%v", secret)
		os.Exit(ERR_NOT_FOUND)
	}

	secretDataMap := secret.Data["data"]

	md, ok := secretDataMap.(map[string]interface{})
	if !ok {
		fmt.Printf("unable to find value for %s, exiting  \n", dataName)
		os.Exit(ERR_NOT_FOUND)
	}

	secretValue := md[dataName]

	return secretValue.(string)
}

func GetConstraintValue(constraints []*nomadapi.Constraint, constraintName string) string {
	for _, constraint := range constraints {
		if constraint.LTarget == constraintName {
			return constraint.RTarget
		}
	}

	return ""
}

func BuildNomadQuotaKey(quota_key string, constraints []*nomadapi.Constraint) string {
	return fmt.Sprintf("%s%s%s%s%s",
		GetConstraintValue(constraints, NOMAD_ENV_CONSTRAINT),
		NOMAD_QUOTA_KEY_SEPARATOR,
		GetConstraintValue(constraints, NOMAD_GROUP_CONSTRAINT),
		NOMAD_QUOTA_KEY_SEPARATOR,
		quota_key)
}

func ValidateConstraint(constraints []*nomadapi.Constraint, constraintName string) {

	constraintIsPresent := false
	errorMessage := "Missing mandatory constraint: %s, exiting  \n"

	for _, constraint := range constraints {
		if constraint.LTarget == constraintName {
			constraintIsPresent = true
			if constraint.RTarget == "" {
				fmt.Printf(errorMessage, constraintName)
				os.Exit(ERR_NOT_FOUND)
			}
		}
	}

	if !constraintIsPresent {
		fmt.Printf(errorMessage, constraintName)
		os.Exit(ERR_NOT_FOUND)
	}
}

func ExitErrorf(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

func AwsCredentialsCleanup(toClean string) string {
	var re = regexp.MustCompile(`(.+)AWS_ACCESS_KEY_ID=[^\s]+\s(.+)`)
	toClean = re.ReplaceAllString(toClean, `$1.$2`)

	re = regexp.MustCompile(`(.+)AWS_SECRET_ACCESS_KEY=[^\s]+\s(.+)`)
	toClean = re.ReplaceAllString(toClean, `$1.$2`)

	return toClean
}

func GetEnvPath(dataName string, env string) string {
	return dataName + "_" + env
}

func GetDataFromConsulWithPath(dataName string, path string) string {
	client := GetConsulClient()
	kvp, _, err := client.KV().Get(path+dataName, nil)
	if err != nil {
		fmt.Println(err)
	}

	if kvp == nil {
		fmt.Printf("unable to find data in consul for dataName=%s, for path=%s \n", dataName, path)
		return ""
	}

	return string(kvp.Value)
}

func GetKVPairsFromConsulWithPath(path string) consulAPI.KVPairs {
	client := GetConsulClient()
	kvps, _, err := client.KV().List(path, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(ERR_CONSUL_CLIENT)
	}

	if kvps == nil {
		fmt.Printf("unable to find data in consul for path=%s \n", path)
		os.Exit(ERR_NOT_FOUND)
	}

	return kvps
}

func GetServiceAddresses(serviceName string, tags []string, q *consulAPI.QueryOptions) ([]ServiceAddress, uint64, error) {
	client := GetConsulClient()
	health := client.Health()
	if serviceName != "nomad" && serviceName != "consul" {
		tags = nil
	}
	service, metadata, err := health.ServiceMultipleTags(serviceName, tags, true, q)
	lastIndex := metadata.LastIndex
	if err != nil {
		fmt.Println(err)
		os.Exit(ERR_CONSUL_CLIENT)
	}
	lengthOfServiceAddress := len(service)
	serviceAddresses := make([]ServiceAddress, 0)
	for i := 0; i < lengthOfServiceAddress; i++ {

		serviceAddr := ServiceAddress{
			Host: service[i].Service.Address,
			Port: service[i].Service.Port,
			Node: service[i].Node.Node,
		}
		serviceAddresses = append(serviceAddresses, serviceAddr)
	}

	return serviceAddresses, lastIndex, err
}

func GetAWSConfigFromVault(AWS_KEY_ID string, AWS_ACCESS_KEY string, env string) (*AWSConfig, error) {

	awskid := GetDataFromVault(GetEnvPath(AWS_KEY_ID, env))
	awssak := GetDataFromVault(GetEnvPath(AWS_ACCESS_KEY, env))

	return &AWSConfig{
		AccessKeyID:     awskid,
		SecretAccessKey: awssak,
		SecurityToken:   "",
	}, nil
}

func (c *AWSConfig) GetALBClient(region string) (*elbv2.ELBV2, error) {
	creds := c.GetCredentials()

	cfg := &aws.Config{
		Region:      aws.String(region),
		Credentials: creds,

		// We will do our own retrying
		MaxRetries: aws.Int(0),
	}

	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %s", err)
	}

	return elbv2.New(sess), nil
}

func (c *AWSConfig) GetEc2Client(region string) (*ec2.EC2, error) {
	creds := c.GetCredentials()

	cfg := &aws.Config{
		Region:      aws.String(region),
		Credentials: creds,

		// We will do our own retrying
		MaxRetries: aws.Int(0),
	}

	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %s", err)
	}

	return ec2.New(sess), nil
}

func (c *AWSConfig) GetCredentials() *credentials.Credentials {
	providers := make([]credentials.Provider, 0, 2)

	if c != nil {
		providers = append(providers, &credentials.StaticProvider{
			Value: credentials.Value{
				AccessKeyID:     c.AccessKeyID,
				SecretAccessKey: c.SecretAccessKey,
			},
		})
	}

	providers = append(providers, &credentials.EnvProvider{})

	cfg := &aws.Config{}
	metadataClient := ec2metadata.New(session.New(cfg))
	providers = append(providers, &ec2rolecreds.EC2RoleProvider{
		Client: metadataClient,
	})

	return credentials.NewChainCredentials(providers)
}
