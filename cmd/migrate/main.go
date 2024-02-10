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

var token = flag.String("token", os.Getenv("NOTION_TOKEN"), "notion token")
var databaseID = flag.String("db", os.Getenv("NOTION_DATABASE_ID"), "database to migrate")
var pageID = flag.String("pageID", os.Getenv("NOTION_PAGE_ID"), "page to migrate")
var pagePropertiesList = flag.String("page-properties", "", "the page propeties to convert to frontmater")
var obsidianVault = flag.String("vault", os.Getenv("OBSIDIAN_VAULT_PATH"), "Obsidian vault location")
var destination = flag.String("d", "", "Destination to store pages within Obsidian Vault")
var filenameFromPage = flag.String("name", "Name", "Page attribute to extract the file name. Support selecting different page attribute and formatting. By default we use the page name")
var storeImages = flag.Bool("i", false, "download external images to Obsidan vault and link in the resulting page")

func main() {
	flag.Parse()

	if empty(token) {
		flag.Usage()
		fmt.Println("You must provide the notion token to run the script")
		os.Exit(1)
	}

	if empty(databaseID) && empty(pageID) {
		flag.Usage()
		fmt.Println("You must provide a notion database ID or a page ID to run the script")
		os.Exit(1)
	}

	if !empty(databaseID) && !empty(pageID) {
		flag.Usage()
		fmt.Println("You must provide a notion database ID or a page ID not both to run the script")
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
		fmt.Println("You must provide the obisidian vault path to run the script")
		os.Exit(1)
	}

	if empty(destination) {
		flag.Usage()
		fmt.Println("You must provide the destination path to run the script")
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
		Token:                   *token,
		DatabaseID:              *databaseID,
		PageID:                  *pageID,
		StoreImages:             *storeImages,
		PageNameFilters:         pageNameFilters,
		PagePropertiesToMigrate: pageProperties,
		VaultPath:               *obsidianVault,
		VaultDestination:        *destination,
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

	var jobs []queue.Job

	q := queue.NewQueue("migrating notion pages")

	for _, page := range pages {
		// We need to do this, because variables declared in for loops are passed by reference.
		// Otherwise, our closure will always receive the last item from the page.
		newPage := page

		path := migrator.ExtractPageTitle(newPage)

		job := queue.Job{
			Path: path,
			Run: func() error {
				return migrator.FetchParseAndSavePage(ctx, page, config.PagePropertiesToMigrate, path, true)
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
