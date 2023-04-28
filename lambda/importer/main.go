package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	jira "github.com/andygrunwald/go-jira"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v52/github"
	"github.com/shopwarelabs/gh-import-slack/lambda/shared"
	"github.com/slack-go/slack"
	"github.com/trivago/tgo/tcontainer"
	"github.com/xanzy/go-gitlab"
)

var api = slack.New(os.Getenv("SLACK_BOT_TOKEN"))

func HandleRequest(event events.SQSEvent) {
	for _, record := range event.Records {
		var queueMessage shared.ImportMessage

		if err := json.Unmarshal([]byte(record.Body), &queueMessage); err != nil {
			log.Printf("Error while unmarshalling message: %s", err.Error())
			continue
		}

		handleMessage(queueMessage, context.Background())
	}
}

func handleMessage(queueMessage shared.ImportMessage, ctx context.Context) {
	_, _, err := api.PostMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Importing your PR...", false))

	if err != nil {
		log.Printf("Error while sending message: %s", err.Error())
		return
	}

	slackUser, err := api.GetUserInfoContext(ctx, queueMessage.SlackUser)

	if err != nil {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while fetching user: "+err.Error(), false))
		return
	}

	mapping, ok := shared.GetRepositoryMapping()[queueMessage.Repository.GetFullName()]

	if !ok {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText(fmt.Sprintf("This repository %s cannot be imported as it is unknown", queueMessage.Repository.GetFullName()), false))
		return
	}

	githubPem, _ := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PEM"))
	installationID, err := strconv.ParseInt(os.Getenv("GITHUB_INSTALLATION_ID"), 10, 64)
	if err != nil {
		log.Printf("Error while parsing installation ID: %s", err.Error())
		return
	}

	appID, err := strconv.ParseInt(os.Getenv("GITHUB_APP_ID"), 10, 64)
	if err != nil {
		log.Printf("Error while parsing app ID: %s", err.Error())
		return
	}

	tr, err := ghinstallation.New(http.DefaultTransport, appID, installationID, []byte(githubPem))

	if err != nil {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while creating GitHub client: "+err.Error(), false))
		return
	}

	client := github.NewClient(&http.Client{Transport: tr})
	prID, err := strconv.Atoi(queueMessage.Repository.ID)
	if err != nil {
		log.Printf("Error while converting PR ID to integer: %s", err.Error())
		return
	}

	pr, _, err := client.PullRequests.Get(ctx, queueMessage.Repository.Owner, queueMessage.Repository.Repo, prID)

	if err != nil {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while fetching PR: "+err.Error(), false))
		return
	}

	prCommits, _, err := client.PullRequests.ListCommits(ctx, queueMessage.Repository.Owner, queueMessage.Repository.Repo, prID, nil)

	if err != nil {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while fetching PR commits: "+err.Error(), false))
		return
	}

	if queueMessage.JiraTicket == "" {
		jiraTicket, err := createJiraTicket(pr, mapping, queueMessage.Team)

		if err != nil {
			api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while creating Jira ticket: "+err.Error(), false))
			return
		}

		queueMessage.JiraTicket = jiraTicket
	}

	localBranch, err := importBranch(pr, prCommits, queueMessage, mapping, slackUser)

	if err != nil {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while importing branch: "+err.Error(), false))
		return
	}

	log.Printf("Imported branch %s", localBranch)

	mr, err := createMergeRequest(pr, localBranch, queueMessage, mapping)

	if err != nil {
		api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText("Error while creating merge request: "+err.Error(), false))
		return
	}

	api.SendMessageContext(ctx, queueMessage.SlackUser, slack.MsgOptionText(fmt.Sprintf("Created merge request: %s\nJira ticket: %s/browse/%s", mr.WebURL, os.Getenv("JIRA_HOST"), queueMessage.JiraTicket), false))

	body := `Hello,

thank you for creating this pull request.
I have opened an issue on our Issue Tracker for you. See the issue link: https://issues.shopware.com/issues/%s

Please use this issue to track the state of your pull request.`

	client.Issues.CreateComment(ctx, queueMessage.Repository.Owner, queueMessage.Repository.Repo, prID, &github.IssueComment{
		Body: aws.String(fmt.Sprintf(body, queueMessage.JiraTicket)),
	})

	client.Issues.AddLabelsToIssue(ctx, queueMessage.Repository.Owner, queueMessage.Repository.Repo, prID, []string{"Scheduled"})
}

func createMergeRequest(pr *github.PullRequest, localBranch string, queueMessage shared.ImportMessage, mapping shared.RepositoryMapping) (*gitlab.MergeRequest, error) {
	client, _ := gitlab.NewClient(os.Getenv("GITLAB_PASSWORD"), gitlab.WithBaseURL(os.Getenv("GITLAB_URL")))

	mr, resp, err := client.MergeRequests.CreateMergeRequest(mapping.GitlabProjectId, &gitlab.CreateMergeRequestOptions{
		Title:              aws.String(fmt.Sprintf("%s - %s", queueMessage.JiraTicket, pr.GetTitle())),
		Description:        aws.String(pr.GetBody()),
		SourceBranch:       aws.String(localBranch),
		TargetBranch:       aws.String(pr.GetBase().GetRef()),
		Labels:             &gitlab.Labels{"github"},
		TargetProjectID:    aws.Int(mapping.GitlabProjectId),
		RemoveSourceBranch: aws.Bool(true),
		Squash:             aws.Bool(false),
		AllowCollaboration: aws.Bool(true),
	})

	content, _ := io.ReadAll(resp.Body)

	log.Println(string(content))

	return mr, err
}

func importBranch(pr *github.PullRequest, commits []*github.RepositoryCommit, queueMessage shared.ImportMessage, mapping shared.RepositoryMapping, slackUser *slack.User) (string, error) {
	tmpDir, _ := os.MkdirTemp(os.TempDir(), "importer")

	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("git", "clone", pr.GetBase().GetRepo().GetCloneURL(), tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("error while cloning repository: %s, output: %s", err.Error(), string(output))
	}

	cmd = exec.Command("git", "config", "user.email", slackUser.Profile.Email)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error while setting git config: %s", err.Error())
	}

	cmd = exec.Command("git", "config", "user.name", slackUser.Profile.RealName)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error while setting git config: %s", err.Error())
	}

	remoteName := "github-pr-head"
	remoteBranch := pr.Head.GetRef()
	localBranch := strings.ToLower(queueMessage.JiraTicket) + "/auto-imported-from-github"

	cmd = exec.Command("git", "remote", "add", remoteName, pr.GetHead().GetRepo().GetCloneURL())
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error while adding remote: %s", err.Error())
	}

	cmd = exec.Command("git", "fetch", remoteName, remoteBranch)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error while fetching branch: %s", err.Error())
	}

	cmd = exec.Command("git", "checkout", "-b", localBranch, remoteName+"/"+remoteBranch)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error while checking out branch: %s", err.Error())
	}

	if pr.GetCommits() > 1 {
		cmd = exec.Command("git", "reset", "--soft", commits[0].GetSHA())
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("error while resetting branch: %s", err.Error())
		}
	}

	prevMsg := commits[0].Commit.GetMessage()
	newMsg := ""

	if !strings.HasPrefix(prevMsg, queueMessage.JiraTicket) {
		newMsg = queueMessage.JiraTicket
	}

	newMsg = newMsg + " - " + prevMsg + "\nfixes #" + strconv.Itoa(pr.GetNumber())

	cmd = exec.Command("git", "commit", "--amend", "-m", newMsg)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("error while amending commit: %s %s", err.Error(), string(output))
	}

	cmd = exec.Command("git", "push", "-u", fmt.Sprintf("https://%s:%s@%s", os.Getenv("GITLAB_USERNAME"), os.Getenv("GITLAB_PASSWORD"), mapping.GitlabCloneUrl), localBranch)
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("error while pushing branch: %s %s", err.Error(), string(output))
	}

	return localBranch, nil
}

func createJiraTicket(pr *github.PullRequest, mapping shared.RepositoryMapping, team string) (string, error) {
	tp := &jira.BasicAuthTransport{
		Username: os.Getenv("JIRA_USERNAME"),
		Password: os.Getenv("JIRA_PASSWORD"),
	}

	client, err := jira.NewClient(tp.Client(), os.Getenv("JIRA_HOST"))

	if err != nil {
		return "", err
	}

	issueReq := &jira.Issue{
		Fields: &jira.IssueFields{
			Project: jira.Project{
				Key: mapping.JiraIssueKey,
			},
			Type: jira.IssueType{
				Name: "Bug",
			},
			Labels:      []string{"Github"},
			Summary:     fmt.Sprintf("[Github] %s", pr.GetTitle()),
			Description: fmt.Sprintf("%s\n\n---\n\n%s%s", pr.GetBody(), "Imported from Github. Please see: ", pr.GetHTMLURL()),
			Unknowns: tcontainer.MarshalMap{
				//' Author
				"customfield_12101": pr.User.GetLogin(),
				// PR Link
				"customfield_12100": pr.GetHTMLURL(),
				// Is Public?
				"customfield_10202": map[string]string{"id": "10110"},
				// Team field
				"customfield_12000": map[string]string{"id": team},
			},
		},
	}

	issue, _, err := client.Issue.Create(issueReq)

	if err != nil {
		return "", err
	}

	return issue.Key, nil
}

func main() {
	lambda.Start(HandleRequest)
}
