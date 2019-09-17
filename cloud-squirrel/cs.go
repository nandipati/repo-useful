// A wrapper around nomad, consul and docker .
// Provides additional functionality like quotas, etc.
// @author Lenko Donchev

package main

import (
	"fmt"
	"log"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"./utils"

	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hashicorp/nomad/jobspec"

	"path/filepath"

	consulapi "github.com/hashicorp/consul/api"
	nomadapi "github.com/hashicorp/nomad/api"

	discover "github.com/hashicorp/go-discover"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type (
	Login struct {
		Username string
		Password string
	}

	Service struct {
		Name string
	}
)

const (
	NOMAD_BINARY                              = "nd_actual"
	CONSUL_BINARY                             = "cl_actual"
	AWS_KEY_ID                                = "awskid"
	AWS_ACCESS_KEY                            = "awssak"
	ERR_CERT_UPLOAD                           = 1
	ERR_MISSING_CONFIG_PROPERTY               = 4
	ERR_QUOTA_LIMIT_EXCEEDED                  = 3
	ERR_EXEC_CMD                              = 6
	ERR_DOCKER_BUILDER                        = 2
	ERR_AWS                                   = 5
	ERR_FAILED_TO_RENEW_OR_CREATE_CERTIFICATE = 7
	EXIT_SUCCESS                              = 0

	DOCKER_REGISTRY_KEY = "nexus-docker-reg"
)

func exec_cmd(cmd string) string {
	parts := strings.Fields(cmd)
	head := parts[0]
	parts = parts[1:len(parts)]

	out, err := exec.Command(head, parts...).CombinedOutput()
	fmt.Printf("%s \n", out)
	if err != nil {
		cmd = utils.AwsCredentialsCleanup(cmd)
		fmt.Printf("Error executing command: %s , Err: %s \n", cmd, err)
		os.Exit(ERR_EXEC_CMD)
	}

	return string(out[:])
}

func exec_shell_cmd(cmd string) string {
	out, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	fmt.Printf("%s \n", out)
	if err != nil {
		cmd = utils.AwsCredentialsCleanup(cmd)
		fmt.Printf("Error executing shell command: %s , Error: %s \n", cmd, err)
		os.Exit(ERR_EXEC_CMD)
	}

	return string(out[:])
}

func get_key(key string, consulClient *consulapi.Client) int {
	kvpair, _, err := consulClient.KV().Get(key, nil)

	if kvpair == nil && err == nil { // key doesnt exist
		return 0
	}

	if err != nil {
		fmt.Printf("ERR get key: %s \n", err)
		os.Exit(7)
	}

	val, err := strconv.Atoi(string(kvpair.Value))
	if err != nil {
		fmt.Printf("ERR atoi: %s \n", err)
		os.Exit(10)
	}

	return val
}

func build_cmd_args(args []string) string {
	return strings.Join(args, " ")
}

func checkQuotaUsage(quota_key string, parsedFile *nomadapi.Job, consulAddress string, consulClient *consulapi.Client) {

	quota_key_property := utils.BuildNomadQuotaKey(quota_key, parsedFile.Constraints)

	quota_limit_key := fmt.Sprintf("quotas/limit/%s", quota_key_property)
	quota_usage_key := fmt.Sprintf("quotas/usage/%s", quota_key_property)

	quota_limit := get_key(quota_limit_key, consulClient)
	quota_usage := get_key(quota_usage_key, consulClient)

	requested_resource_amount := 0
	switch quota_key {
	case "cpu":
		requested_resource_amount = *parsedFile.TaskGroups[0].Tasks[0].Resources.CPU
	case "memory":
		requested_resource_amount = *parsedFile.TaskGroups[0].Tasks[0].Resources.MemoryMB
	default:
		fmt.Println("Unexpected quota type:", quota_usage_key)
		os.Exit(12)
	}

	if requested_resource_amount+quota_usage > quota_limit {
		fmt.Printf(quota_key+" limit exceeded. Will not start job. Quota limit=%d . Quota key:%s \n", quota_limit, quota_usage_key)
		os.Exit(ERR_QUOTA_LIMIT_EXCEEDED)
	}
}

func checkNodeClass(nodeClassFromNomadJobFile, nodeClassFromPropertiesFile string, isValidNodeClass map[string]bool) {
	if !strings.Contains(nodeClassFromPropertiesFile, nodeClassFromNomadJobFile) {
		fmt.Printf(" Node class from nomad jobfile(%s) doesnt contain node class from properties file(%s). Please put supported node class in the job file. Exiting. \n",
			nodeClassFromNomadJobFile, nodeClassFromPropertiesFile)

		os.Exit(ERR_MISSING_CONFIG_PROPERTY)
	}

	if !isValidNodeClass[nodeClassFromNomadJobFile] {
		fmt.Printf("Unsupported node class:(%s). Please put supported node class in the job file. Exiting. \n",
			nodeClassFromNomadJobFile)

		os.Exit(ERR_MISSING_CONFIG_PROPERTY)
	}
}

func checkNomadJobFile(job_file string, consulAddress string, consulClient *consulapi.Client, isValidNodeClass map[string]bool) (string, []Service) {
	path, err := filepath.Abs(job_file)
	if err != nil {
		fmt.Printf(" Unable to open nomad job file: %s, Error:  %s \n", job_file, err)
		os.Exit(1)
	}

	parsedFile, err := jobspec.ParseFile(path)
	if err != nil {
		fmt.Printf("Unable to parse nomad job file: %s, Error:%s \n", job_file, err)
		os.Exit(2)
	}

	utils.ValidateConstraint(parsedFile.Constraints, utils.NOMAD_GROUP_CONSTRAINT)
	utils.ValidateConstraint(parsedFile.Constraints, utils.NOMAD_ENV_CONSTRAINT)

	checkQuotaUsage("cpu", parsedFile, consulAddress, consulClient)
	checkQuotaUsage("memory", parsedFile, consulAddress, consulClient)

	checkNodeClass(utils.GetConstraintValue(parsedFile.Constraints,
		"${node.class}"), utils.GetConfigString("node_class"), isValidNodeClass)

	servicesArrayInJob := make([]Service, 0)
	taskGroups := parsedFile.TaskGroups

	for _, taskGroup := range taskGroups {
		tasks := taskGroup.Tasks
		for _, task := range tasks {
			servicesInTask := task.Services
			for _, serviceInTask := range servicesInTask {
				seviceObj := Service{
					Name: serviceInTask.Name,
				}
				servicesArrayInJob = append(servicesArrayInJob, seviceObj)
			}
		}
	}

	return path, servicesArrayInJob
}

func buildNomadCommand() string {
	return " export NOMAD_ADDR=" + utils.GetConfigString("nomad_server") + " && " + NOMAD_BINARY
}

func main() {

	isValidNodeClass := map[string]bool{
		"dev":   true,
		"uat":   true,
		"test":  true,
		"stage": true,
		"prod":  true,
	}

	var Tag string
	var Directory string
	var File string
	viper.SetConfigName("cs") // name of config file (without extension)
	viper.AddConfigPath("$HOME/.cs")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	consulAddress := utils.GetConfigString("consul_server")
	vaultAddress := utils.GetConfigString("vault_address")
	vaultToken := utils.GetDataFromConsul(utils.VAULT_ACCESS_TOKEN_KEY_NAME_IN_CONSUL)
	vaultRole := utils.GetConfigString("vault_role")
	datacenter := utils.GetConfigString("consul_datacenter")
	awsEnv := utils.GetConfigString("env")

	config := consulapi.DefaultConfig()
	config.Address = consulAddress
	config.Datacenter = datacenter
	config.Token = ""
	consulClient, err := consulapi.NewClient(config)
	if err != nil {
		fmt.Printf("Unable to create client(%v): %v", consulAddress, err)
		os.Exit(1)
	}

	var cmdQuota = &cobra.Command{
		Use:   "quota [quota_sub_command] [quota_key] {limit}",
		Short: "Manage nomad quotas.",
		Long: `Manage nomad quots - set quota limits, see quota usage, etc.
                Example:
                   to set quota limits:
                   cs quota init scoring_cpu_prod 4000
                   to see quota ustilization:
                   cs quota usage`,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			quota_sub_command := args[0]

			switch quota_sub_command {
			case "init":
				exec_cmd(fmt.Sprintf(CONSUL_BINARY+" kv put -http-addr=%s  quotas/limit/%s  %s", consulAddress,
					args[1],
					args[2]))
			case "usage":
				exec_cmd(fmt.Sprintf(CONSUL_BINARY+" kv get -recurse -http-addr=%s quotas ", consulAddress))
			default:
				fmt.Println("Unexpected quota_sub_command:", quota_sub_command)
				os.Exit(13)
			}
		},
	}

	var cmdRun = &cobra.Command{
		Use:   "run [job_file] {args}",
		Short: "Run a nomad job.",
		Long: `Run a nomad job with checks for quota limits.
                If a given limit is exceeded then the job will not run. Limits could be cpu, memory etc.
                   Example:
                   cs run scoring_job.nomad`,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {

			job_file := args[0]
			path, servicesInTask := checkNomadJobFile(job_file, consulAddress, consulClient, isValidNodeClass)

			log.Printf("File Path %", path)

			// if we want to run consul server as a Docker image we need to get the join ip of a standalone instance and put that as -join parameter in the config
			// the reson for this is because for some reason -join by aws tags does not work when running consul server from Docker container
			if strings.Contains(job_file, "consul-server") {
				d := discover.Discover{
					Providers: map[string]discover.Provider{
						"aws": discover.Providers["aws"],
					},
				}

				l := log.New(os.Stderr, "", log.LstdFlags)

				cfg := "provider=aws tag_key=join_tag tag_value=consul-server"
				addrs, err := d.Addrs(cfg, l)
				if err != nil {
					utils.ExitErrorf("Unable to get consul-server IPs from AWS tags. Error: %v", err)
				}

				consulJoinIp := addrs[0]
				exec_shell_cmd(fmt.Sprintf(` sed -i  -e 's|CONSUL_JOIN_IP|%s|'  %s    `, consulJoinIp, job_file))
			}

			exec_shell_cmd(fmt.Sprintf(buildNomadCommand()+" run   %s", build_cmd_args(args)))

			updateTargetGroup(AWS_KEY_ID, AWS_ACCESS_KEY, awsEnv, servicesInTask)
		},
	}

	var cmdRunArtifactID = &cobra.Command{
		Use:   "run-artifact-id [artifact-id] [job_file] {args}",
		Short: "Run a nomad job using a specific version of the Docker artifact.",
		Long: `Run a nomad job with checks for quota limits.
	                If a given limit is exceeded then the job will not run. Limits could be cpu, memory etc.
	                   Example:
	                   cs run scoring_job.nomad`,
		Args: cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {

			job_file := args[1]
			path, servicesInTask := checkNomadJobFile(job_file, consulAddress, consulClient, isValidNodeClass)

			artifactId := args[0]
			exec_shell_cmd(fmt.Sprintf(` sed -i  -e 's|\(image = \".*\)/.*/.*\:.*\"|\1/%s\"|'  %s    `, artifactId, path))

			exec_shell_cmd(fmt.Sprintf(buildNomadCommand()+" run   %s", build_cmd_args(args[1:])))

			updateTargetGroup(AWS_KEY_ID, AWS_ACCESS_KEY, awsEnv, servicesInTask)
		},
	}

	var cmdNomad = &cobra.Command{
		Use:   "nomad ... ",
		Short: "Run any nomad command.",
		Long: `Run any nomad command the same as if running the nomad client itself.
                The only command that is disabled is the "run" command because it has checks
                for limits and that is provided by the cs command.
                    Example:
                    to see the status of the nomad jobs:
                    cs nomad status
                    to stop a nomad job
                    cs nomad stop <jobID>`,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {

			if len(args) > 0 && "run" == args[0] {
				fmt.Printf(" run command is not allowed when directly invoking nomad. Use 'cs run <jobfile> ...'  \n")
				os.Exit(-7)
			}
			exec_shell_cmd(fmt.Sprintf(buildNomadCommand()+"  %s", build_cmd_args(args)))
		},
	}

	var cmdCertificates = &cobra.Command{
		Use:   "cert [cert_sub_command]  {DOMAIN_NAME} {TIME_TO_LIVE}",
		Short: "Manage certificates.",
		Long: `Manage certificates - generate a cert, upload to aws or other cloud providers, etc.
                Example:
                   to generate a certificate:
                   cs cert generate istio.rcscorenp 8700
                   to upload the generated certificate to AWS:
                   cs cert upload`,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cert_sub_command := args[0]

			switch cert_sub_command {
			case "generate":
				exec_cmd(fmt.Sprintf(" create_cert.sh %s %s %s %s %s", vaultAddress, vaultToken, vaultRole,
					args[1],
					args[2]))
			case "upload":
				cloud_provider := "aws"
				if cloud_provider == "aws" {
					awskid := utils.GetDataFromVault(AWS_KEY_ID)
					awssak := utils.GetDataFromVault(AWS_ACCESS_KEY)

					aws_region := utils.GetConfigString("region")
					exec_cmd(fmt.Sprintf(" upload_cert_to_aws.sh %s %s %s", awskid, awssak, aws_region))
				} else {
					fmt.Println("Unsupported cloud provider:", cloud_provider)
					os.Exit(ERR_CERT_UPLOAD)
				}
			default:
				fmt.Println("Unexpected cert_sub_command:", cert_sub_command)
				os.Exit(ERR_CERT_UPLOAD)
			}
		},
	}

	var dockerBuild = &cobra.Command{
		Use:   "builder [builder_sub_command] [image_name] {path} ",
		Short: "Build, push and pull docker images.",
		Long: `push/(build and push) docker images to the artifactory repo called artifact-repo.rcs.rsiapps.io.
		            Examples:
		            to push images
		            builder push alpine
		            to build Dockerfile and push
		            builder build -d path_of_directory -f docker_file_name -t image_tag
		            to pull docker from dockerhub or any repo
		            builder pull {image/tagged_image}`,

		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			pwd := utils.GetDataFromVault(DOCKER_REGISTRY_KEY)
			login := Login{"docker", pwd}

			builder_sub_command := args[0]

			switch builder_sub_command {
			case "push":
				if len(args) < 2 {
					fmt.Println("please provide image name")
					os.Exit(ERR_DOCKER_BUILDER)
				}
				push_repo(args[1], login)

			case "build":
				if len(Directory) > 0 && len(Tag) > 0 && len(File) > 0 {
					dockerCommand := fmt.Sprintf("docker build --no-cache --pull -t %s -f %s %s", Tag, File, Directory)
					fmt.Println(fmt.Sprintf("Executing docker command: %s", dockerCommand))
					exec_cmd(dockerCommand)
					push_repo(Tag, login)
				} else {
					fmt.Println("please provide all the flags -d, -f, -t")
					os.Exit(ERR_DOCKER_BUILDER)
				}
			case "pull":
				if len(args) < 2 {
					fmt.Println("please provide image name")
					os.Exit(ERR_DOCKER_BUILDER)
				}
				repoName := utils.GetConfigString("repoPullName")
				exec_cmd(fmt.Sprintf("docker pull %s/%s", repoName, args[1]))
			default:
				fmt.Println(fmt.Sprintf("applying the docker command -- docker %s", build_cmd_args(args)))
				exec_cmd(fmt.Sprintf("docker %s", build_cmd_args(args)))
				os.Exit(EXIT_SUCCESS)
			}
		},
	}

	var cmdAWS = &cobra.Command{
		Use:   "aws [sub_command]  {options}",
		Short: "Access Amazon AWS API.",
		Long: `Interact with Amazon AWS - get a file from S3 bucket, etc.
                Example:
                   to get a file from AWS S3 bucket:
                   cs aws get-file-from-S3 <bucket_name> <folder_name> <item_name>
               `,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sub_command := args[0]

			switch sub_command {
			case "get-file-from-S3":

				if len(args) != 4 {
					utils.ExitErrorf("Bucket and item names required \nUsage: %s bucket_name folder_name item_name",
						args[0])
				}

				bucket := args[1]
				folder := args[2]
				item := args[3]

				file, err := os.Create(item)
				if err != nil {
					utils.ExitErrorf("Unable to open file %q, %v", err)
				}

				defer file.Close()

				awskid := utils.GetDataFromVault(AWS_KEY_ID)
				awssak := utils.GetDataFromVault(AWS_ACCESS_KEY)
				aws_region := utils.GetConfigString("region")
				sess, _ := session.NewSession(&aws.Config{
					Region:      aws.String(aws_region),
					Credentials: credentials.NewStaticCredentials(awskid, awssak, ""),
				},
				)

				downloader := s3manager.NewDownloader(sess)

				numBytes, err := downloader.Download(file,
					&s3.GetObjectInput{
						Bucket: aws.String(bucket),
						Key:    aws.String(folder + "/" + item),
					})
				if err != nil {
					utils.ExitErrorf("Unable to download item %q, %v", item, err)
				}

				fmt.Println("Downloaded", file.Name(), numBytes, "bytes")

			default:
				fmt.Println("Unsupported sub_command:", sub_command)
				os.Exit(ERR_AWS)
			}
		},
	}

	var cmdLetsEncryptCertificates = &cobra.Command{
		Use:   "lets-encrypt [sub_command]  {options}",
		Short: " Lets Encrypt certificate management API.",
		Long: `Interact with Lets Encrypt certificate management API - generate a certificate, renew cert, etc.
                Example:
                   to get a new certificate:
                   cs lets-encrypt gen-or-renew-cert-and-upload-to-aws <domain> <email> <env>
                   to renew existing certificate:
                   cs lets-encrypt gen-or-renew-cert-and-upload-to-aws <domain> <email> <env> [cert_arn]
               `,
		Args: cobra.MinimumNArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			sub_command := args[0]

			switch sub_command {
			case "gen-or-renew-cert-and-upload-to-aws":

				if len(args) < 4 {
					utils.ExitErrorf("Expected 4 or 5 args")
				}

				domain := args[1]
				email := args[2]
				env := args[3]
				cert_arn := ""

				awskid := utils.GetDataFromVault(utils.GetEnvPath(AWS_KEY_ID, env))
				awssak := utils.GetDataFromVault(utils.GetEnvPath(AWS_ACCESS_KEY, env))

				aws_region := utils.GetConfigString("region")

				lets_encrypt_root_dir := utils.GetConfigString("lets_encrypt_root_dir")

				//STAGING env variable - 	If this variable is set at all, hit the Let's Encrypt Staging environment instead of the real one. Only use this for testing, as the certificates will not be valid.
				// https://hub.docker.com/r/ntcnvisia/certbot-route53/
				var letsEncryptStaging string
				if os.Getenv("LETS_ENCRYPT_STAGING") != "" {
					letsEncryptStaging = "  -e \"STAGING=true\"  "
				}

				output := exec_shell_cmd(fmt.Sprintf(" docker run  %s  -e \"DOMAIN=%s\" -e \"EMAIL=%s\" -e \"AWS_ACCESS_KEY_ID=%s\" -e \"AWS_SECRET_ACCESS_KEY=%s\" -e \"AWS_DEFAULT_REGION=%s\" -e \"TZPATH=America/Chicago\" -v %s:/etc/letsencrypt artifact-repo.service.rcsnp.rsiapps.internal:6070/rcs/letsencrypt-certbot:1.0.0", letsEncryptStaging, domain, email, awskid, awssak, aws_region, lets_encrypt_root_dir))

				if strings.Contains(output, "not yet due for renewal") {
					fmt.Printf("\n Certificate for domain %s  not yet due for renewal. \n", domain)
					os.Exit(utils.EXIT_SUCCESS)
				}

				re := regexp.MustCompile("/etc/letsencrypt/live/(.+)/fullchain.pem")
				matches := re.FindStringSubmatch(output)
				if !(len(matches[1]) > 0) {
					fmt.Printf("\n Error: Unexpected output for Certificate for domain %s  .  Output: %s \n", domain, output)
					os.Exit(utils.ERR_CERT_UPLOAD)
				}

				fmt.Printf("\n  Certificate for domain %s  .  Saved at root dir: %s \n", domain, matches[1])

				lets_encrypt_cert_domain_root := lets_encrypt_root_dir + "/live/" + matches[1] + "/"
				cert_path := lets_encrypt_cert_domain_root + "cert.pem"
				private_key_path := lets_encrypt_cert_domain_root + "privkey.pem"
				chain_path := lets_encrypt_cert_domain_root + "chain.pem"

				importCertCommand := " export AWS_ACCESS_KEY_ID=%s  &&   export AWS_SECRET_ACCESS_KEY=%s && sudo -E  aws acm import-certificate --region=%s --certificate file://%s --private-key file://%s --certificate-chain file://%s   "
				if len(args) > 4 {
					cert_arn = args[4]
					importCertCommand = importCertCommand + " --certificate-arn %s "
					output = exec_shell_cmd(fmt.Sprintf(importCertCommand, awskid, awssak, aws_region, cert_path, private_key_path, chain_path, cert_arn))
				} else {
					output = exec_shell_cmd(fmt.Sprintf(importCertCommand, awskid, awssak, aws_region, cert_path, private_key_path, chain_path))
				}

				if strings.Contains(output, "CertificateArn") {
					fmt.Printf("\n Succsessfully created or renewed certificate for domain %s !!! \n", domain)
					os.Exit(EXIT_SUCCESS)
				} else {
					fmt.Printf("\n Failed to create or renew certificate for domain %s ! \n", domain)
					os.Exit(ERR_FAILED_TO_RENEW_OR_CREATE_CERTIFICATE)
				}

			default:
				fmt.Println("Unsupported sub_command:", sub_command)
				os.Exit(ERR_AWS)
			}
		},
	}

	var rootCmd = &cobra.Command{Use: "cs"}

	rootCmd.AddCommand(cmdQuota)
	rootCmd.AddCommand(cmdRun)
	rootCmd.AddCommand(cmdRunArtifactID)
	rootCmd.AddCommand(cmdNomad)

	dockerBuild.Flags().StringVarP(&Tag, "tag", "t", "", "Tag the docker image")
	dockerBuild.Flags().StringVarP(&Directory, "directory", "d", "", "directory to run the docker")
	dockerBuild.Flags().StringVarP(&File, "file", "f", "", "file to use to build image")

	rootCmd.AddCommand(dockerBuild)

	rootCmd.AddCommand(cmdLetsEncryptCertificates)
	rootCmd.AddCommand(cmdCertificates)
	rootCmd.AddCommand(cmdAWS)

	rootCmd.Execute()
}

func push_repo(image_name string, login Login) {
	repoName := utils.GetConfigString("repoPushName")
	exec_cmd(fmt.Sprintf("docker tag %s %s/%s", image_name, repoName, image_name))
	exec_cmd(fmt.Sprintf("docker login -u %s -p %s %s", login.Username, login.Password, repoName))
	exec_cmd(fmt.Sprintf("docker push %s/%s", repoName, image_name))
}
