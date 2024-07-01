package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/shopwarelabs/gh-import-slack/lambda/shared"
	"github.com/slack-go/slack"
)

var awsSession *session.Session

var prUrlRegexp = regexp.MustCompile(`(?m)^https:\/\/github.com\/([\w-]*)\/([\w-]*)\/pull\/(\d*)$`)

var api = slack.New(os.Getenv("SLACK_BOT_TOKEN"))

func HandleRequest(req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	content := req.Body

	if req.IsBase64Encoded {
		contentByte, _ := base64.StdEncoding.DecodeString(content)
		content = string(contentByte)
	}

	str, _ := url.QueryUnescape(content)
	str = strings.Replace(str, "payload=", "", 1)

	var message slack.InteractionCallback
	if err := json.Unmarshal([]byte(str), &message); err != nil {
		return events.LambdaFunctionURLResponse{
			StatusCode: 400,
			Body:       "Bad Request",
		}, nil
	}

	switch message.Type {
	case slack.InteractionTypeShortcut:
		prLinkInput := slack.NewTextInput("prLink", "PR Link", "")
		prLinkInput.Subtype = "url"
		prLinkInput.Placeholder = "https://github.com/shopware/shopware/pull/1"
		prTeamInput := slack.NewStaticSelectDialogInput("prTeam", "Team", []slack.DialogSelectOption{
			{
				Label: "CT Core",
				Value: "12610",
			},
			{
				Label: "CT Administration",
				Value: "12611",
			},
			{
				Label: "CT Storefront",
				Value: "12612",
			},
			{
				Label: "Runtime Terror",
				Value: "12606",
			},
			{
				Label: "Ctrl Alt Elite",
				Value: "12609",
			},
			{
				Label: "Byte Club",
				Value: "12608",
			},
			{
				Label: "Kung Fu Coders",
				Value: "12816",
			},
			{
				Label: "Codebusters",
				Value: "12607",
			},
			{
				Label: "Golden Stars",
				Value: "11900",
			},
			{
				Label: "Golden Plus",
				Value: "12657",
			},
			{
				Label: "Stranger Codes",
				Value: "11928",
			},
			{
				Label: "Barware",
				Value: "12400",
			},
		})
		prTeamInput.Optional = false

		prJiraTicketInput := slack.NewTextInput("prJiraTicket", "Jira Ticket (if existing)", "")
		prJiraTicketInput.Optional = true
		prJiraTicketInput.Placeholder = "NEXT-1234"

		dialog := slack.Dialog{
			CallbackID:  "import_pr",
			Title:       "Import GitHub PR",
			SubmitLabel: "Import",
			Elements:    []slack.DialogElement{prLinkInput, prTeamInput, prJiraTicketInput},
		}

		api.OpenDialog(message.TriggerID, dialog)

		return events.LambdaFunctionURLResponse{
			StatusCode: 200,
			Body:       "",
		}, nil
	case slack.InteractionTypeDialogSubmission:
		prLink := message.Submission["prLink"]

		if !prUrlRegexp.MatchString(prLink) {
			log.Printf("Got invalid PR link at dialog submission: %s", prLink)

			text, _ := json.Marshal(slack.DialogInputValidationErrors{
				Errors: []slack.DialogInputValidationError{
					{
						Name:  "prLink",
						Error: "Invalid PR Link",
					},
				}},
			)

			return events.LambdaFunctionURLResponse{
				StatusCode: 200,
				Body:       string(text),
			}, nil
		}

		log.Printf("Got valid PR link at dialog submission: %s", prLink)

		matches := prUrlRegexp.FindStringSubmatch(prLink)

		message := shared.ImportMessage{
			Repository: shared.ImportPullRequest{
				Repo:  matches[2],
				Owner: matches[1],
				ID:    matches[3],
			},
			Team:       message.Submission["prTeam"],
			JiraTicket: message.Submission["prJiraTicket"],
			SlackUser:  message.User.ID,
		}

		messageJson, _ := json.Marshal(message)

		queueInput := &sqs.SendMessageInput{
			MessageBody: aws.String(string(messageJson)),
			QueueUrl:    aws.String(os.Getenv("SQS_IMPORT_QUEUE")),
		}

		log.Printf("Sending message to queue: %s", string(messageJson))

		msg, err := sqs.New(awsSession).SendMessage(queueInput)
		if err != nil {
			log.Printf("Error while sending message to queue: %s", err.Error())
		}

		log.Printf("Message sent to queue: %s", *msg.MessageId)

		return events.LambdaFunctionURLResponse{
			StatusCode: 200,
			Body:       "",
		}, nil
	}

	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Body:       "Invalid Request",
	}, nil
}

func main() {
	awsSession, _ = session.NewSession()
	lambda.Start(HandleRequest)
}
