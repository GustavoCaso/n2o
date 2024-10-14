package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/GustavoCaso/n2o/internal/log"
	"github.com/GustavoCaso/n2o/internal/migrator"
	"github.com/GustavoCaso/n2o/internal/queue"
)

var filenameFromPageExplanation = `Notion page properties to extract the Obsidian page title. 
Support selecting different page attributes and formatting. To select multiple properties, use a comma-separated list.
The attributes that support custom formatting are Notion date attributes.
Example of how to use a Notion date property with custom format as the title for the Obsidian page:
-name=date:%Y/%B/%d-%A

By default if you do not configure any we get the Title property 
`

var pagePropertiesExplanation = `Notion page properties to convert to Obsidian frontmater.
You can select multiple properties using a comma-separated list.
`

var notionToken = flag.String("notion-token", os.Getenv("N2O_NOTION_TOKEN"), "Notion token")
var notionDatabaseID = flag.String("notion-db-ID", os.Getenv("N2O_NOTION_DATABASE_ID"), "Notion database to migrate")
var notionPageID = flag.String("notion-page-ID", os.Getenv("N2O_NOTION_PAGE_ID"), "Notion page to migrate")
var pagePropertiesList = flag.String("page-properties", "", pagePropertiesExplanation)
var filenameFromPage = flag.String("page-name", "", filenameFromPageExplanation)
var obsidianVault = flag.String("vault-path", os.Getenv("N2O_OBSIDIAN_VAULT_PATH"), "Obsidian vault location")
var vaultDestination = flag.String("vault-folder", "", "folder to store pages inside the Obsidian Vault")
var storeImages = flag.Bool("download-images", false, "download external images to the Obsidian vault")
var saveToDisk = flag.Bool("save-to-disk", false, "write the pages in the Obsidian vault")
var debug = flag.Bool("debug", false, "print debug information")

func main() {
	flag.Parse()

	logger := log.New(os.Stdout)

	if empty(notionToken) {
		flag.Usage()
		logger.Warn("You must provide the notion token")
		os.Exit(1)
	}

	if empty(notionDatabaseID) && empty(notionPageID) {
		flag.Usage()
		logger.Warn("You must provide a notion database ID or a page ID")
		os.Exit(1)
	}

	if !empty(notionDatabaseID) && !empty(notionPageID) {
		flag.Usage()
		logger.Warn("You must provide a notion database ID or a page ID not both")
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
		logger.Warn("You must provide the Obisidian vault path")
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

	config := &config.Config{
		Token:                   *notionToken,
		DatabaseID:              *notionDatabaseID,
		PageID:                  *notionPageID,
		StoreImages:             *storeImages,
		PageNameFilters:         pageNameFilters,
		PagePropertiesToMigrate: pageProperties,
		VaultPath:               *obsidianVault,
		VaultDestination:        *vaultDestination,
		SaveToDisk:              *saveToDisk,
		Debug:                   *debug,
	}

	ctx := context.Background()
	buf := &bytes.Buffer{}
	migratorLogger := log.New(buf)

	migrator := migrator.NewMigrator(config, migrator.NewCache(), migratorLogger)

	pages, err := migrator.FetchPages(ctx)
	if err != nil {
		logger.Error(fmt.Sprintf("an error ocurred when fetching page. error: %v\n", err))
		os.Exit(1)
	}

	var jobs []*queue.Job

	q := queue.NewQueue("fetching notion pages information", queue.WithProgressBar())

	for _, page := range pages {
		// We need to do this, because variables declared inside for loops are passed by reference.
		// Otherwise, our closure will always receive the last item from the page.
		newPage := page

		job := &queue.Job{
			Path: newPage.Path,
			Run: func() error {
				return migrator.FetchParseAndSavePage(ctx, newPage, config.PagePropertiesToMigrate)
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

	migratorLogs, _ := io.ReadAll(buf)

	fmt.Fprint(os.Stdout, string(migratorLogs))

	for _, errJob := range worker.ErrorJobs {
		logger.Error(fmt.Sprintf("an error ocurred when processing a page %s. error: %v\n", errJob.Job.Path, errors.Unwrap(errJob.Err)))
	}

	if config.SaveToDisk {
		logger.Info("Saving pages to the Obsidian vault")
		err := migrator.WritePagesToDisk(ctx)
		if err != nil {
			logger.Error(fmt.Sprintf("an error ocurred when writing pages to the Obsidian vault. error: %v\n", err))
			os.Exit(1)
		}
	} else {
		logger.Info("Displaying the pages that would be created in your vault")
		err := migrator.DisplayInformation(ctx)
		if err != nil {
			logger.Error(fmt.Sprintf("an error ocurred when displaying the pages information. error: %v\n", err))
			os.Exit(1)
		}
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("Do you want to write the pages to the Obsidian vault? y/n")
		text, _ := reader.ReadString('\n')
		if strings.Contains(strings.ToLower(text), "y") {
			logger.Info("Saving pages to the Obsidian vault")
			err := migrator.WritePagesToDisk(ctx)
			if err != nil {
				logger.Error(fmt.Sprintf("an error ocurred when writing pages to the Obsidian vault. error: %v\n", err))
				os.Exit(1)
			}
		}
	}

	logger.Info("Done 🎉")
}

func empty(v *string) bool {
	return *v == ""
}
