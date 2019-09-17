package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"

	"./utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
	consul "github.com/hashicorp/consul/api"
)

type TargetGroup struct {
	arn               string
	awsRegion         string
	albClient         *elbv2.ELBV2
	ec2Client         *ec2.EC2
	consulServiceName string
	consulClient      *consul.Client
}

type TargetGroupConfig struct {
	TargetGroupARN string
	ServiceName    string
	DatacenterName string
}

type Target struct {
	InstanceId string
	Port       int
}

func (t *Target) AsALBTarget() *elbv2.TargetDescription {
	return &elbv2.TargetDescription{
		Id:   aws.String(t.InstanceId),
		Port: aws.Int64(int64(t.Port)),
	}
}

// Get All the Services Target Groups and try to update with latest Host and Port
func updateTargetGroups(AWS_KEY_ID string, AWS_ACCESS_KEY string, env string) {

	var err error

	awsConfig, err := utils.GetAWSConfigFromVault(AWS_KEY_ID, AWS_ACCESS_KEY, env)

	if err != nil {
		log.Fatal(err)
		os.Exit(13)
	}

	datacenter := utils.GetConfigString("consul_datacenter")

	kvps := utils.GetKVPairsFromConsulWithPath("service/apps/targetgroups/")

	for _, pair := range kvps {

		ServiceName := pair.Key
		targetGroupARN := string(pair.Value)
		targetGroupConfig := &TargetGroupConfig{
			TargetGroupARN: targetGroupARN,
			ServiceName:    ServiceName,
			DatacenterName: datacenter,
		}

		targetGroup, error := NewTargetGroup(targetGroupConfig, awsConfig)

		if error != nil {
			log.Fatal(error)
		}
		if error == nil {
			targetGroup.update_target_group()
		}
	}
}

//Get Services changed and get Target Groups and try to update with latest Host and Port
func updateTargetGroup(AWS_KEY_ID string, AWS_ACCESS_KEY string, env string, servicesInTask []Service) {

	var err error

	awsConfig, err := utils.GetAWSConfigFromVault(AWS_KEY_ID, AWS_ACCESS_KEY, env)

	if err != nil {
		log.Fatal(err)
		os.Exit(13)
	}

	datacenter := utils.GetConfigString("consul_datacenter")

	for _, serviceInTask := range servicesInTask {

		targetGroupConfig := NewTargetGroupConfig(serviceInTask.Name, datacenter)

		if targetGroupConfig.TargetGroupARN != "" {

			time.Sleep(1 * time.Minute)

			targetGroup, error := NewTargetGroup(targetGroupConfig, awsConfig)

			if error != nil {
				log.Fatal(error)
			}
			if error == nil {
				targetGroup.update_target_group()
			}
		}
	}
}

func GetTargetGroupForService(serviceName string) string {
	return utils.GetDataFromConsulWithPath(serviceName, "service/apps/targetgroups/")
}

func AWSRegion(TargetGroupARN string) string {

	// The region is embedded in the ARN, assuming that we've been given
	// a valid and complete ARN. If not, we'll just return the empty string
	// and let the caller signal an error.
	parts := strings.Split(TargetGroupARN, ":")

	if len(parts) < 6 || parts[0] != "arn" {
		// Doesn't seem like a valid ARN
		return ""
	}

	return parts[3]
}

func NewTargetGroupConfig(ServiceName string, DatacenterName string) *TargetGroupConfig {

	targetGroupARN := GetTargetGroupForService(ServiceName)

	return &TargetGroupConfig{
		TargetGroupARN: targetGroupARN,
		ServiceName:    ServiceName,
		DatacenterName: DatacenterName,
	}
}

func NewTargetGroup(config *TargetGroupConfig, awsConfig *utils.AWSConfig) (*TargetGroup, error) {

	consulClient := utils.GetConsulClient()
	awsRegion := AWSRegion(config.TargetGroupARN)
	if awsRegion == "" {
		return nil, fmt.Errorf("ARN malformed, so couldn't extract region name")
	}

	albClient, err := awsConfig.GetALBClient(awsRegion)

	if err != nil {
		return nil, err
	}

	ec2Client, err := awsConfig.GetEc2Client(awsRegion)

	if err != nil {
		return nil, err
	}

	return &TargetGroup{
		arn:               config.TargetGroupARN,
		awsRegion:         awsRegion,
		albClient:         albClient,
		ec2Client:         ec2Client,
		consulServiceName: config.ServiceName,
		consulClient:      consulClient,
	}, nil
}

func (tg *TargetGroup) update_target_group() {
	log.Printf("Syncing Consul service %q to ALB target group %q", tg.consulServiceName, tg.arn)

	consulSet := tg.getTargetSet()
	albSet, err := tg.getCurrentAlbTargets()
	if err != nil {
		log.Printf("failed to get current targets for %q: %s", tg.arn, err)
	}

	allSet := consulSet.Union(albSet)

	toAdd := allSet.Subtract(albSet)
	toRemove := allSet.Subtract(consulSet)

	// We'll deal with adding first, since in a catastrophic
	// failure situation it's adding things that is more likely
	// to restore service, and the ALB itself has probably already
	// noticed that the downed targets are down.

	if len(toAdd) > 0 {
		err = tg.AddTargets(toAdd)
		if err != nil {
			log.Printf("failed to add targets %s to %q: %s", toAdd, tg.arn, err)
			log.Printf("skipping removal of %s from %q due to earlier add failure", toAdd, tg.arn)
		}
		log.Printf("added %s to %q", toAdd, tg.arn)
	}

	if len(toRemove) > 0 {
		err = tg.RemoveTargets(toRemove)
		if err != nil {
			log.Printf("failed to remove targets %s from %q: %s", toRemove, tg.arn, err)
		}
		log.Printf("removed %s from %q", toRemove, tg.arn)
	}
}

func (tg *TargetGroup) getTargetSet() TargetSet {
	queryOpts := &consul.QueryOptions{
		WaitIndex: 0,
	}
	serviceName := tg.consulServiceName
	var tags = []string{"http"}
	services, lastIndex, err := utils.GetServiceAddresses(serviceName, tags, queryOpts)
	if err != nil {
		log.Printf(
			"Error inspecting Consul service %q for target group %q: %s",
			serviceName,
			tg.arn,
			err,
		)
		// Wait a bit so we don't hammer Consul while it's unwell
		//time.Sleep(15 * time.Second)
	}

	queryOpts.WaitIndex = lastIndex

	m := make(TargetSet)
	for _, service := range services {
		// The Consul node name is assumed to be the instance id
		//instanceID, err := tg.getEc2IntanceID("private-ip-address", service.Host)
		if err != nil {
			log.Printf(
				"Error when getting Instance Id of %q for Host %q: %s",
				serviceName,
				service.Host,
				err,
			)
		}
		//m.Add(instanceID, service.Port)
		m.Add(service.Host, service.Port)
	}

	return m
}

func (tg *TargetGroup) KeepSyncing() {
	log.Printf("Syncing Consul service %q to ALB target group %q", tg.consulServiceName, tg.arn)

	servicesChan := tg.watchConsulService()

	for {
		select {
		case consulSet := <-servicesChan:
			albSet, err := tg.getCurrentAlbTargets()
			if err != nil {
				log.Printf("failed to get current targets for %q: %s", tg.arn, err)
				// FIXME: Should we retry a bit here, rather than waiting
				// until next time Consul tells us about a change?
				continue
			}

			allSet := consulSet.Union(albSet)

			toAdd := allSet.Subtract(albSet)
			toRemove := allSet.Subtract(consulSet)

			// We'll deal with adding first, since in a catastrophic
			// failure situation it's adding things that is more likely
			// to restore service, and the ALB itself has probably already
			// noticed that the downed targets are down.

			if len(toAdd) > 0 {
				err = tg.AddTargets(toAdd)
				if err != nil {
					log.Printf("failed to add targets %s to %q: %s", toAdd, tg.arn, err)

					// If we failed to add and then we remove, we might end
					// up leaving the load balancer in a state where it has
					// no instances, so to be safe we'll wait until we can
					// successfully add before we start removing.
					log.Printf("skipping removal of %s from %q due to earlier add failure", toAdd, tg.arn)
					continue
				}
				log.Printf("added %s to %q", toAdd, tg.arn)
			}

			if len(toRemove) > 0 {
				err = tg.RemoveTargets(toRemove)
				if err != nil {
					log.Printf("failed to remove targets %s from %q: %s", toRemove, tg.arn, err)
				}
				log.Printf("removed %s from %q", toRemove, tg.arn)
			}
		}
	}
}

func (tg *TargetGroup) watchConsulService() <-chan TargetSet {
	queryOpts := &consul.QueryOptions{
		WaitIndex: 0,
	}
	serviceName := tg.consulServiceName
	ret := make(chan TargetSet)

	var tags = []string{"http"}
	go func() {
		for {
			services, lastIndex, err := utils.GetServiceAddresses(serviceName, tags, queryOpts)
			if err != nil {
				log.Printf(
					"Error inspecting Consul service %q for target group %q: %s",
					serviceName,
					tg.arn,
					err,
				)
				// Wait a bit so we don't hammer Consul while it's unwell
				//time.Sleep(15 * time.Second)
				continue
			}

			queryOpts.WaitIndex = lastIndex

			m := make(TargetSet)
			for _, service := range services {
				// The Consul node name is assumed to be the instance id
				//instanceID, err := tg.getEc2IntanceID("private-ip-address", service.Host)
				if err != nil {
					log.Printf(
						"Error when getting Instance Id of %q for Host %q: %s",
						serviceName,
						service.Host,
						err,
					)
				}
				//m.Add(instanceID, service.Port)
				m.Add(service.Host, service.Port)
			}

			ret <- m
		}
	}()

	// FIXME: Currently the caller has no way to stop watching
	// once it's started. For now this is okay because we keep
	// watching until we quit anyway, but this would prevent us
	// from doing e.g. graceful configuration reloading on SIGHUP
	// in future.
	return ret
}

func (tg *TargetGroup) getEc2IntanceID(name string, filter string) (string, error) {
	svc := tg.ec2Client
	filters := []*ec2.Filter{
		&ec2.Filter{
			Name: aws.String(name),
			Values: []*string{
				aws.String(filter),
			},
		},
	}
	input := ec2.DescribeInstancesInput{Filters: filters}
	result, err := svc.DescribeInstances(&input)
	if err != nil {
		panic(err.Error())
	}

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			return *instance.InstanceId, nil
		}
	}

	return "", nil
}

func (tg *TargetGroup) getCurrentAlbTargets() (TargetSet, error) {
	ret := make(TargetSet)

	resp, err := tg.albClient.DescribeTargetHealth(&elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(tg.arn),
	})
	if err != nil {
		return nil, err
	}

	for _, hd := range resp.TargetHealthDescriptions {
		if *hd.TargetHealth.State == "draining" {
			// Ignore a target that we've already told to leave
			continue
		}

		ret.Add(*hd.Target.Id, int(*hd.Target.Port))
	}

	return ret, nil
}

func (tg *TargetGroup) AddTargets(set TargetSet) error {
	if len(set) == 0 {
		return nil
	}
	_, err := tg.albClient.RegisterTargets(&elbv2.RegisterTargetsInput{
		TargetGroupArn: aws.String(tg.arn),
		Targets:        set.AsALBTargetList(),
	})
	return err
}

func (tg *TargetGroup) RemoveTargets(set TargetSet) error {
	if len(set) == 0 {
		return nil
	}
	_, err := tg.albClient.DeregisterTargets(&elbv2.DeregisterTargetsInput{
		TargetGroupArn: aws.String(tg.arn),
		Targets:        set.AsALBTargetList(),
	})
	return err
}

func (t *Target) String() string {
	return fmt.Sprintf("%s:%d", t.InstanceId, t.Port)
}

type TargetSet map[Target]struct{}

func (s TargetSet) Add(InstanceId string, Port int) {
	s[Target{InstanceId, Port}] = struct{}{}
}

func (s TargetSet) AddTarget(t Target) {
	s[t] = struct{}{}
}

func (s TargetSet) Has(InstanceId string, Port int) bool {
	_, ok := s[Target{InstanceId, Port}]
	return ok
}

func (s TargetSet) HasTarget(t Target) bool {
	_, ok := s[t]
	return ok
}

func (s TargetSet) Union(other TargetSet) TargetSet {
	ret := make(TargetSet)

	for k, v := range s {
		ret[k] = v
	}
	for k, v := range other {
		ret[k] = v
	}

	return ret
}

func (s TargetSet) Subtract(other TargetSet) TargetSet {
	ret := make(TargetSet)

	for k, _ := range s {
		if !other.HasTarget(k) {
			ret.AddTarget(k)
		}
	}

	return ret
}

func (s TargetSet) AsALBTargetList() []*elbv2.TargetDescription {
	ret := make([]*elbv2.TargetDescription, 0, len(s))

	for target := range s {
		ret = append(ret, target.AsALBTarget())
	}

	return ret
}

func (s TargetSet) String() string {
	strs := make([]string, 0, len(s))

	for target := range s {
		strs = append(strs, target.String())
	}

	return fmt.Sprintf("{%s}", strings.Join(strs, ", "))
}
