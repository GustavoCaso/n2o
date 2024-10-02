package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GustavoCaso/n2o/internal/cache"
	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/GustavoCaso/n2o/internal/migrator"
	"github.com/GustavoCaso/n2o/internal/queue"
)

var filenameFromPageExplanation = `Notion page properties to extract the Obsidian page title. 
Support selecting different page attributes and formatting. To select multiple properties, use a comma-separated list.
The attributes that support custom formatting are Notion date attributes.
Example of how to use a Notion date property with custom format as the title for the Obsidian page:
-name=date:%Y/%B/%d-%A
`

var pagePropertiesExplanation = `Notion page properties to convert to Obsidian frontmater.
You can select multiple properties using a comma-separated list.
`

var notionToken = flag.String("notion-token", os.Getenv("N2O_NOTION_TOKEN"), "Notion token")
var notionDatabaseID = flag.String("notion-db-ID", os.Getenv("N2O_NOTION_DATABASE_ID"), "Notion database to migrate")
var notionPageID = flag.String("notion-page-ID", os.Getenv("N2O_NOTION_PAGE_ID"), "Notion page to migrate")
var pagePropertiesList = flag.String("page-properties", "", pagePropertiesExplanation)
var filenameFromPage = flag.String("page-name", "Name", filenameFromPageExplanation)
var obsidianVault = flag.String("vault-path", os.Getenv("N2O_OBSIDIAN_VAULT_PATH"), "Obsidian vault location")
var vaultDestination = flag.String("vault-folder", "", "folder to store pages inside the Obsidian Vault")
var storeImages = flag.Bool("download-images", false, "download external images to the Obsidian vault")

func main() {
	flag.Parse()

	if empty(notionToken) {
		flag.Usage()
		fmt.Println("You must provide the notion token")
		os.Exit(1)
	}

	if empty(notionDatabaseID) && empty(notionPageID) {
		flag.Usage()
		fmt.Println("You must provide a notion database ID or a page ID")
		os.Exit(1)
	}

	if !empty(notionDatabaseID) && !empty(notionPageID) {
		flag.Usage()
		fmt.Println("You must provide a notion database ID or a page ID not both")
		os.Exit(1)
	}

	pageProperties := map[string]bool{}

	if !empty(pagePropertiesList) {
		results := strings.Split(*pagePropertiesList, ",")
		for _, prop := range results {
			pageProperties[prop] = true
		}
	}

	if empty(obsidianVault) {
		flag.Usage()
		fmt.Println("You must provide the Obisidian vault path")
		os.Exit(1)
	}

	if empty(vaultDestination) {
		flag.Usage()
		fmt.Println("You must provide the Obsidian vault folder")
		os.Exit(1)
	}

	pageNameFilters := map[string]string{}
	if !empty(filenameFromPage) {
		pagePathResults := strings.Split(*filenameFromPage, ",")
		for _, pagePathAttribute := range pagePathResults {
			pageWithFormatOptions := strings.Split(pagePathAttribute, ":")
			if len(pageWithFormatOptions) > 1 {
				pageNameFilters[strings.ToLower(pageWithFormatOptions[0])] = pageWithFormatOptions[1]
			} else {
				pageNameFilters[strings.ToLower(pageWithFormatOptions[0])] = ""
			}
		}
	}

	config := config.Config{
		Token:                   *notionToken,
		DatabaseID:              *notionDatabaseID,
		PageID:                  *notionPageID,
		StoreImages:             *storeImages,
		PageNameFilters:         pageNameFilters,
		PagePropertiesToMigrate: pageProperties,
		VaultPath:               *obsidianVault,
		VaultDestination:        *vaultDestination,
	}

	ctx := context.Background()

	migrator := migrator.NewMigrator(config, cache.NewCache())

	err := migrator.Setup()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	pages, err := migrator.FetchPages(ctx)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	var jobs []*queue.Job

	q := queue.NewQueue("migrating notion pages", queue.WithProgressBar())

	for _, page := range pages {
		// We need to do this, because variables declared inside for loops are passed by reference.
		// Otherwise, our closure will always receive the last item from the page.
		newPage := page

		path := migrator.ExtractPageTitle(newPage)

		job := &queue.Job{
			Path: path,
			Run: func() error {
				return migrator.FetchParseAndSavePage(ctx, page, config.PagePropertiesToMigrate, path)
			},
		}

		jobs = append(jobs, job)
	}

	// enequeue page to download and parse
	q.AddJobs(jobs)

	worker := queue.Worker{
		Queue: q,
	}

	worker.DoWork()

	for _, errJob := range worker.ErrorJobs {
		fmt.Printf("an error ocurred when processing a page %s. error: %v\n", errJob.Job.Path, errors.Unwrap(errJob.Err))
	}
}

func empty(v *string) bool {
	return *v == ""
}
