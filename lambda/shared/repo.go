package shared

type RepositoryMapping struct {
	JiraIssueKey    string
	GitlabProjectId int
	GitlabCloneUrl  string
}

func GetRepositoryMapping() map[string]RepositoryMapping {
	return map[string]RepositoryMapping{
		"shopware/platform": {
			JiraIssueKey:    "NEXT",
			GitlabProjectId: 1,
			GitlabCloneUrl:  "gitlab.shopware.com/shopware/6/product/platform.git",
		},
		"shopware/SwagPayPal": {
			JiraIssueKey:    "PPI",
			GitlabProjectId: 7,
			GitlabCloneUrl:  "gitlab.shopware.com:shopware/6/services/paypal.git",
		},
		"shopware/SwagMigrationConnector": {
			JiraIssueKey:    "MIG",
			GitlabProjectId: 102,
			GitlabCloneUrl:  "gitlab.shopware.com:shopware/5/services/swagmigrationconnector.git",
		},
		"shopware/SwagMigrationMagento": {
			JiraIssueKey:    "MIG",
			GitlabProjectId: 69,
			GitlabCloneUrl:  "gitlab.shopware.com/shopware/6/services/swagmigrationmagento.git",
		},
	}
}
