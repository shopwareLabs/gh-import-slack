package shared

type ImportMessage struct {
	Repository ImportPullRequest `json:"repository"`
	Team       string            `json:"team"`
	JiraTicket string            `json:"jira_ticket"`
	SlackUser  string            `json:"slack_user"`
}

type ImportPullRequest struct {
	Repo  string `json:"repo"`
	Owner string `json:"owner"`
	ID    string `json:"id"`
}

func (i ImportPullRequest) GetFullName() string {
	return i.Owner + "/" + i.Repo
}
