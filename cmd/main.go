package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/gin-gonic/gin"

	"fmt"

	"github.com/jasonlvhit/gocron"
)

// TODO:
// better logging
// circuit-breaker logic

// healthcheck listener port
var Port = "8080"

// remote activity server port
var asPort = "18800"

// instances we've already seen
var instancesSeen = []string{}

type activeSessions struct {
	TotalActiveSessions2hrs int `json:"total_active_sessions_2hr"`
}

func GetRunningInstances(client *ec2.EC2) (*ec2.DescribeInstancesOutput, error) {
	result, err := client.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("instance-state-name"),
				Values: []*string{
					aws.String("running"),
				},
			},
			{
				Name: aws.String("tag:Role"),
				Values: []*string{
					aws.String("remote_dev"),
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	return result, err
}

func terminateInstance(client *ec2.EC2, instanceId string) error {
	_, err := client.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: []*string{&instanceId},
	})

	return err
}

func readSessions(url string) int {
	httpClient := http.Client{Timeout: time.Second * 2}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Println(err)
		// return a bogus value so we don't cause a panic:
		return 1
	}
	res, getErr := httpClient.Do(req)
	if getErr != nil {
		log.Println(getErr)
		// return a bogus value so we don't cause a panic:
		return 1
	}

	if res.Body != nil {
		defer res.Body.Close()
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Println(readErr)
		// return a bogus value so we don't cause a panic:
		return 1
	}
	activeSessions := activeSessions{}
	jsonErr := json.Unmarshal(body, &activeSessions)
	if jsonErr != nil {
		log.Println(jsonErr)
		// return a bogus value so we don't cause a panic:
		return 1
	}
	return activeSessions.TotalActiveSessions2hrs
}

func sliceContains(sl []string, name string) bool {
	for _, value := range sl {
		if value == name {
			return true
		}
	}
	return false
}

func terminate_instances(AWSRegion string) error {
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(AWSRegion),
		},
	})

	if err != nil {
		fmt.Printf("Failed to initialize new session: %v", err)
		return err
	}

	ec2Client := ec2.New(sess)

	runningInstances, err := GetRunningInstances(ec2Client)
	if err != nil {
		fmt.Printf("Couldn't retrieve running instances: %v", err)
		return err
	}

	for _, reservation := range runningInstances.Reservations {
		for _, instance := range reservation.Instances {
			if !sliceContains(instancesSeen, *instance.InstanceId) {
				log.Printf("New %s instance %s with IP %s detected", AWSRegion, *instance.InstanceId, *instance.PublicIpAddress)
				instancesSeen = append(instancesSeen, *instance.InstanceId)
			}
			url := fmt.Sprintf("http://%s:%s/sessions", *instance.PublicIpAddress, asPort)
			totalActiveSessions := readSessions(url)
			if totalActiveSessions == 0 {
				log.Printf("Terminating %s instance %s with 0 sessions for the last 2 hours\n", AWSRegion, *instance.InstanceId)
				err = terminateInstance(ec2Client, *instance.InstanceId)
				if err != nil {
					log.Printf("Failed to terminate instance %s: %v", *instance.InstanceId, err)
					return err
				}
			}
		}
	}
	return err
}

func poll_all_regions() {
	var awsRegions = []string{"us-east-1", "us-west-1", "ap-south-1", "ap-southeast-1"}
	for _, awsRegion := range awsRegions {
		err := terminate_instances(awsRegion)
		if err != nil {
			log.Printf("ERROR: unable to terminate instances in %s", awsRegion)
		}
	}
	return
}

// run a background gorouting every 60 seconds
func executeCronJob() {
	gocron.Every(60).Second().Do(poll_all_regions)
	<-gocron.Start()
}

func main() {
	log.Println("#### instance terminator starting up!")
	go executeCronJob()
	// create an endpoint
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.Run(":" + Port)
}
