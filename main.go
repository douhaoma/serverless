package main

import (
	"cloud.google.com/go/storage"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/mailgun/mailgun-go/v3"
	"google.golang.org/api/option"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

// SubmissionMessage defines the structure of the JSON data in the SNS message.
type SubmissionMessage struct {
	StudentEmail   string `json:"student_email"`
	SubmissionURL  string `json:"submission_url"`
	AssignmentName string `json:"assignment_name"`
}

func Handler(ctx context.Context, snsEvent events.SNSEvent) {
	snsRecord := snsEvent.Records[0].SNS
	var msg SubmissionMessage
	err := json.Unmarshal([]byte(snsRecord.Message), &msg)
	if err != nil {
		fmt.Println("Error while unmarshalling SNS message:", err)
		return
	}
	email := msg.StudentEmail
	fileURL := msg.SubmissionURL
	assignment := msg.AssignmentName
	fmt.Println(assignment, "Submission URL is:", msg.SubmissionURL)

	// 从 url 下载
	status := 1
	googleCredentials := os.Getenv("GOOGLE_CREDENTIALS")
	decodedCredentials, err := base64.StdEncoding.DecodeString(googleCredentials)
	bucketName := os.Getenv("BUCKET_NAME")
	emailApiKey := os.Getenv("MAILGUN_API")
	emailDomain := os.Getenv("EMAIL_DOMAIN")
	tableName := os.Getenv("DYNAMO_TABLE")

	// 下载文件
	resp, err := http.Get(fileURL)
	if err != nil {
		fmt.Println("Download file error:", err)
		status = 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("failed to download file: HTTP status ", resp.StatusCode)
		status = 0
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("failed to read downloaded content:", err)
		status = 0
	}

	// GCP 认证和上传文件
	client, err := storage.NewClient(ctx, option.WithCredentialsJSON(decodedCredentials))
	if err != nil {
		fmt.Println("storage.NewClient: ", err)
		status = 0
	}
	defer client.Close()

	// 写入 GCP Bucket
	wc := client.Bucket(bucketName).Object(assignment + email + " submission").NewWriter(ctx)
	if _, err = wc.Write(body); err != nil {
		fmt.Println("failed to write to GCP Bucket: ", err)
		status = 0
	}
	if err := wc.Close(); err != nil {
		fmt.Println("failed to close GCP Bucket writer: ", err)
		status = 0
	}
	fmt.Println(status)

	// 发送邮件通知
	var id string
	if status == 1 {
		id, err = SendSuccessMessage(emailDomain, emailApiKey, email, fileURL, assignment)
	} else {
		id, err = SendFailMessage(emailDomain, emailApiKey, email, fileURL, assignment)
	}

	fmt.Println(id, err)

	err = TrackEmailSent(id, email, tableName, status)
	if err != nil {
		fmt.Println("track email error ", err)
	}
}

type EmailInfo struct {
	SentID       string `json:"sentId"`
	EmailAddress string `json:"toEmailAddress"`
	Status       int    `json:"status"`
	Time         string `json:"time"`
}

func TrackEmailSent(sentId, email, tableName string, status int) error {
	// Create a new session and DynamoDB client
	sess := session.Must(session.NewSession())
	svc := dynamodb.New(sess)

	// Get the current time in string format
	currentTime := time.Now().Format(time.RFC3339)

	// Create an instance of EmailInfo with the provided data
	emailInfo := EmailInfo{
		SentID:       sentId,
		EmailAddress: email,
		Status:       status,
		Time:         currentTime,
	}

	// Marshal the EmailInfo data into a map[string]*dynamodb.AttributeValue
	item, err := dynamodbattribute.MarshalMap(emailInfo)
	if err != nil {
		log.Printf("Error marshalling item: %s\n", err)
		return err
	}
	log.Printf("Marshalled item: %+v\n", item)
	// Create the PutItem input
	input := &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(tableName),
	}

	// Put the item in the DynamoDB table
	_, err = svc.PutItem(input)
	if err != nil {
		log.Printf("Error putting item to DynamoDB: %s\n", err)
		return err
	}
	log.Println("Successfully put item to DynamoDB")
	return nil
}

func SendSuccessMessage(domain, apiKey, emailAddress, submissionUrl, assignment string) (string, error) {
	mg := mailgun.NewMailgun(domain, apiKey)
	m := mg.NewMessage(
		"mailgun@demo.douhaoma.me",
		"Assignment Submission Confirmation: "+assignment,
		"Dear "+emailAddress+",\n\n"+
			"We are pleased to inform you that your assignment titled "+assignment+" has been successfully submitted: "+submissionUrl+
			"Thank you for completing the work on time. Your dedication is essential to the progression of our course.\n\n"+
			"Should you have any questions or require further assistance, please do not hesitate to reach out.\n\n"+
			"Wishing you continued success in your studies!\n\n\n"+
			"Warm regards,\n"+
			"Automatically Sent by Assignment Submission System",
		emailAddress,
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_, id, err := mg.Send(ctx, m)
	return id, err
}

func SendFailMessage(domain, apiKey, emailAddress, submissionUrl, assignment string) (string, error) {
	mg := mailgun.NewMailgun(domain, apiKey)
	m := mg.NewMessage(
		"mailgun@demo.douhaoma.me",
		"Action Required: Assignment Submission Failed",
		"Dear "+emailAddress+",\n\n"+
			"It appears that there was an issue with your recent attempt to submit the assignment"+assignment+
			"Unfortunately, we did not receive your submission successfully. The URL you provided for your submission is: "+submissionUrl+".\n\n"+
			"Please review the submission link or file for any errors and attempt to submit again. If the problem persists, do not hesitate to contact us for support.\n\n"+
			"We understand that technical issues can be frustrating and appreciate your patience in resolving this matter.\n\n"+
			"Best regards,\n"+
			"Automatically Sent by Assignment Submission System",
		emailAddress,
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	_, id, err := mg.Send(ctx, m)
	return id, err
}
func main() {
	// Make the handler available for Remote Procedure Call by AWS Lambda
	lambda.Start(Handler)
}
