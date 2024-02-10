package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/GustavoCaso/n2o/internal/cache"
	"github.com/GustavoCaso/n2o/internal/queue"
	"github.com/dstotijn/go-notion"
	"github.com/itchyny/timefmt-go"
)

var mentionCache = cache.NewCache()

type pathAttributes map[string]string

var databaseIDUsage = `notion database ID to migrate.
If you want to specify the propeties to convert to frontmater use a colon and provide a comma separated list. Ex ID:name,date
If you rather want to provide a skip list separate the ID and the skip list using >. Ex ID>day of the week,date
`

var token = flag.String("token", os.Getenv("NOTION_TOKEN"), "notion token")
var databaseID = flag.String("db", os.Getenv("NOTION_DATABASE_ID"), databaseIDUsage)
var pageID = flag.String("pageID", os.Getenv("NOTION_PAGE_ID"), "page to downwload")
var pagePropertiesList = flag.String("page-properties", "", "the page propeties to convert to frontmater")
var obsidianVault = flag.String("vault", os.Getenv("OBSIDIAN_VAULT_PATH"), "Obsidian vault location")
var destination = flag.String("d", "", "Destination to store pages within Obsidian Vault")
var pagePath = flag.String("path", "", "Page path in which to store the pages. Support selecting different page attribute and formatting")
var storeImages = flag.Bool("i", false, "download external images to Obsidan vault and link in the resulting page")
var obsidianVaultImagePath string

func vaultDestination() string {
	return filepath.Join(*obsidianVault, *destination)
}

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

	pagePathFilters := pathAttributes{}
	if !empty(pagePath) {
		pagePathResults := strings.Split(*pagePath, ",")
		for _, pagePathAttribute := range pagePathResults {
			pageWithFormatOptions := strings.Split(pagePathAttribute, ":")
			if len(pageWithFormatOptions) > 1 {
				pagePathFilters[strings.ToLower(pageWithFormatOptions[0])] = pageWithFormatOptions[1]
			} else {
				pagePathFilters[strings.ToLower(pageWithFormatOptions[0])] = ""
			}
		}
	}

	if *storeImages {
		obsidianVaultImagePath = filepath.Join(*obsidianVault, "Images")
		err := os.MkdirAll(obsidianVaultImagePath, 0770)
		if err != nil {
			fmt.Println("failed to create image folder")
			os.Exit(1)
		}
	}

	client := notion.NewClient(*token)

	var pages []notion.Page

	if !empty(databaseID) {
		pages, _ = fetchNotionDBPages(client, *databaseID)
	} else {
		page, err := client.FindPageByID(context.Background(), *pageID)
		if err != nil {
			fmt.Printf("failed to find the page. make sure the page exists in your Notioin workspace. error: %s\n", err.Error())
			os.Exit(1)
		}
		pages = []notion.Page{
			page,
		}
	}

	var jobs []queue.Job

	q := queue.NewQueue("migrating notion pages")

	for _, page := range pages {
		// We need to do this, because variables declared in for loops are passed by reference.
		// Otherwise, our closure will always receive the last item from the page.
		newPage := page

		path := filePath(newPage, pagePathFilters)

		job := queue.Job{
			Path: path,
			Run: func() error {
				return fetchAndSaveToObsidianVault(client, newPage, pageProperties, path, true)
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

func filePath(page notion.Page, pagePathProperties pathAttributes) string {
	properties := page.Properties.(notion.DatabasePageProperties)
	var str string

	for key, value := range properties {
		val, ok := pagePathProperties[strings.ToLower(key)]
		if ok {
			switch value.Type {
			case notion.DBPropTypeDate:
				date := value.Date.Start
				if val != "" {
					str += timefmt.Format(date.Time, val)
				}
			case notion.DBPropTypeTitle:
				str += extractPlainTextFromRichText(value.Title)
			default:
				panic("not suported")
			}
		}
	}

	fileName := fmt.Sprintf("%s.md", str)
	return path.Join(vaultDestination(), fileName)
}

func fetchNotionDBPages(client *notion.Client, id string) ([]notion.Page, error) {
	notionResponse, err := client.QueryDatabase(context.Background(), id, nil)
	if err != nil {
		panic(err)
	}

	result := []notion.Page{}

	result = append(result, notionResponse.Results...)

	query := &notion.DatabaseQuery{}
	for notionResponse.HasMore {
		query.StartCursor = *notionResponse.NextCursor

		notionResponse, err = client.QueryDatabase(context.Background(), id, query)
		if err != nil {
			panic(err)
		}

		result = append(result, notionResponse.Results...)
	}

	return result, nil
}

func fetchAndSaveToObsidianVault(client *notion.Client, page notion.Page, pageProperties map[string]bool, obsidianPath string, dbPage bool) error {
	pageBlocks, err := client.FindBlockChildrenByID(context.Background(), page.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", page.ID, err)
	}

	if err := os.MkdirAll(filepath.Dir(obsidianPath), 0770); err != nil {
		return fmt.Errorf("failed to create the necessary directories in for the Obsidian vault.  error: %w", err)
	}

	f, err := os.Create(obsidianPath)
	if err != nil {
		return fmt.Errorf("failed to create the markdown file %s. error: %w", path.Base(obsidianPath), err)
	}

	defer f.Close()

	// create new buffer
	buffer := bufio.NewWriter(f)

	if dbPage {
		props := page.Properties.(notion.DatabasePageProperties)

		frotmatterProps := make(notion.DatabasePageProperties)

		for propName, propValue := range props {
			if pageProperties[strings.ToLower(propName)] {
				frotmatterProps[propName] = propValue
			}
		}

		if len(frotmatterProps) > 0 {
			propertiesToFrontMatter(frotmatterProps, buffer)
		}
	}

	err = pageToMarkdown(client, pageBlocks.Results, buffer, false)

	if err != nil {
		return fmt.Errorf("failed to convert page to markdown. error: %w", err)
	}

	if err = buffer.Flush(); err != nil {
		return fmt.Errorf("failed to write into the markdown file %s. error: %w", path.Base(obsidianPath), err)
	}

	return nil
}

func pageToMarkdown(client *notion.Client, blocks []notion.Block, buffer *bufio.Writer, indent bool) error {
	var err error

	for _, object := range blocks {
		switch block := object.(type) {
		case *notion.Heading1Block:
			if indent {
				buffer.WriteString("	# ")
			} else {
				buffer.WriteString("# ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.Heading2Block:
			if indent {
				buffer.WriteString("	## ")
			} else {
				buffer.WriteString("## ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.Heading3Block:
			if indent {
				buffer.WriteString("	### ")
			} else {
				buffer.WriteString("### ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.ToDoBlock:
			if indent {
				if *block.Checked {
					buffer.WriteString("	- [x] ")
				} else {
					buffer.WriteString("	- [ ] ")
				}
			} else {
				if *block.Checked {
					buffer.WriteString("- [x] ")
				} else {
					buffer.WriteString("- [ ] ")
				}
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.ParagraphBlock:
			if len(block.RichText) > 0 {
				if indent {
					buffer.WriteString("	")
					if err = writeRichText(client, buffer, block.RichText); err != nil {
						return err
					}
				} else {
					if err = writeRichText(client, buffer, block.RichText); err != nil {
						return err
					}
				}
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.BulletedListItemBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.NumberedListItemBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.CalloutBlock:
			if indent {
				buffer.WriteString("	> [!")
			} else {
				buffer.WriteString("> [!")
			}
			if len(*block.Icon.Emoji) > 0 {
				buffer.WriteString(*block.Icon.Emoji)
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("]")
			buffer.WriteString("\n")
		case *notion.ToggleBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.QuoteBlock:
			if indent {
				buffer.WriteString("	> ")
			} else {
				buffer.WriteString("> ")
			}
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.FileBlock:
			if block.Type == notion.FileTypeExternal {
				if indent {
					buffer.WriteString(fmt.Sprintf("	![](%s)", block.External.URL))
				} else {
					buffer.WriteString(fmt.Sprintf("![](%s)", block.External.URL))
				}
			}
			buffer.WriteString("\n")
		case *notion.DividerBlock:
			buffer.WriteString("---")
			buffer.WriteString("\n")
		case *notion.ChildPageBlock:
			if indent {
				buffer.WriteString(fmt.Sprintf(" [[%s]]", block.Title))
			} else {
				buffer.WriteString(fmt.Sprintf("[[%s]]", block.Title))
			}
			buffer.WriteString("\n")
		case *notion.LinkToPageBlock:
			err := fetchPage(client, block.PageID, "", buffer)
			if err != nil {
				return err
			}
			buffer.WriteString("\n")
		case *notion.CodeBlock:
			buffer.WriteString("```")
			buffer.WriteString(*block.Language)
			buffer.WriteString("\n")
			if err = writeRichText(client, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			buffer.WriteString("```")
			buffer.WriteString("\n")
		case *notion.ImageBlock:
			if block.Type == notion.FileTypeExternal {
				if indent {
					buffer.WriteString(fmt.Sprintf("	![](%s)", block.External.URL))
				} else {
					buffer.WriteString(fmt.Sprintf("![](%s)", block.External.URL))
				}
				buffer.WriteString("\n")
			}
			if block.Type == notion.FileTypeFile && *storeImages {
				name := block.ID() + ".png"
				err := downloadImage(name, block.File.URL)

				if err != nil {
					fmt.Printf("failed to download image. error %s\n", err.Error())
				}

				if indent {
					buffer.WriteString(fmt.Sprintf("	![[Images/%s]]", name))
				} else {
					buffer.WriteString(fmt.Sprintf("![[Images/%s]]", name))
				}
				buffer.WriteString("\n")
			}

		case *notion.VideoBlock:
			if block.Type == notion.FileTypeExternal {
				if indent {
					buffer.WriteString(fmt.Sprintf("	![](%s)", block.External.URL))
				} else {
					buffer.WriteString(fmt.Sprintf("![](%s)", block.External.URL))
				}
			}
			buffer.WriteString("\n")
		case *notion.EmbedBlock:
			if indent {
				buffer.WriteString(fmt.Sprintf("	![](%s)", block.URL))
			} else {
				buffer.WriteString(fmt.Sprintf("![](%s)", block.URL))
			}
			buffer.WriteString("\n")
		case *notion.BookmarkBlock:
			if indent {
				buffer.WriteString(fmt.Sprintf("	![](%s)", block.URL))
			} else {
				buffer.WriteString(fmt.Sprintf("![](%s)", block.URL))
			}
			buffer.WriteString("\n")
		case *notion.ChildDatabaseBlock:
			if indent {
				buffer.WriteString(fmt.Sprintf("	%s", block.Title))
			} else {
				buffer.WriteString(block.Title)
			}
			buffer.WriteString("\n")
		case *notion.ColumnListBlock:
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.ColumnBlock:
			if err = writeChrildren(client, object, buffer); err != nil {
				return err
			}
		case *notion.TableBlock:
			if err = writeTable(client, block.TableWidth, object, buffer); err != nil {
				return err
			}
		case *notion.EquationBlock:
			if indent {
				buffer.WriteString(fmt.Sprintf(" $$%s$$", block.Expression))
			} else {
				buffer.WriteString(fmt.Sprintf("$$%s$$", block.Expression))
			}
			buffer.WriteString("\n")
		case *notion.UnsupportedBlock:
		default:
			return fmt.Errorf("block not supported: %+v", block)
		}
	}

	return nil
}

func downloadImage(name, url string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// We should not worry about checking if the file exists
	// it has been created by the caller
	file, err := os.Create(filepath.Join(obsidianVaultImagePath, name))
	if err != nil {
		return err
	}
	defer file.Close()

	// Use io.Copy to just dump the response body to the file. This supports huge files
	_, err = io.Copy(file, response.Body)
	if err != nil {
		return err
	}
	return nil
}

func propertiesToFrontMatter(propertites notion.DatabasePageProperties, buffer *bufio.Writer) {
	buffer.WriteString("---\n")
	for key, value := range propertites {
		switch value.Type {
		case notion.DBPropTypeTitle:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, extractPlainTextFromRichText(value.Title)))
		case notion.DBPropTypeRichText:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, extractPlainTextFromRichText(value.RichText)))
		case notion.DBPropTypeNumber:
			buffer.WriteString(fmt.Sprintf("%s: %f\n", key, *value.Number))
		case notion.DBPropTypeSelect:
			if value.Select != nil {
				buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.Select.Name))
			}
		case notion.DBPropTypeMultiSelect:
			options := []string{}
			for _, option := range value.MultiSelect {
				options = append(options, option.Name)
			}

			buffer.WriteString(fmt.Sprintf("%s: [%s]\n", key, strings.Join(options[:], ",")))
		case notion.DBPropTypeDate:
			if value.Date != nil {
				if value.Date.Start.HasTime() {
					buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.Date.Start.Format("2006-01-02T15:04:05")))
				} else {
					buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.Date.Start.Format("2006-01-02")))
				}
			}
		case notion.DBPropTypePeople:
		case notion.DBPropTypeFiles:
		case notion.DBPropTypeCheckbox:
			buffer.WriteString(fmt.Sprintf("%s: %t\n", key, *value.Checkbox))
		case notion.DBPropTypeURL:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, *value.URL))
		case notion.DBPropTypeEmail:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, *value.Email))
		case notion.DBPropTypePhoneNumber:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, *value.PhoneNumber))
		case notion.DBPropTypeStatus:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.Status.Name))
		case notion.DBPropTypeFormula:
		case notion.DBPropTypeRelation:
		case notion.DBPropTypeRollup:
			switch value.Rollup.Type {
			case notion.RollupResultTypeNumber:
				buffer.WriteString(fmt.Sprintf("%s: %f\n", key, *value.Rollup.Number))
			case notion.RollupResultTypeDate:
				if value.Rollup.Date.Start.HasTime() {
					buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.Rollup.Date.Start.Format("2006-01-02T15:04:05")))
				} else {
					buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.Rollup.Date.Start.Format("2006-01-02")))
				}
			}
		case notion.DBPropTypeCreatedTime:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.CreatedTime.String()))
		case notion.DBPropTypeCreatedBy:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.CreatedBy.Name))
		case notion.DBPropTypeLastEditedTime:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.LastEditedTime.String()))
		case notion.DBPropTypeLastEditedBy:
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, value.LastEditedBy.Name))
		default:
		}
	}
	buffer.WriteString("---\n")
}

func writeChrildren(client *notion.Client, block notion.Block, buffer *bufio.Writer) error {
	if block.HasChildren() {
		pageBlocks, err := client.FindBlockChildrenByID(context.Background(), block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", block.ID(), err)
		}
		return pageToMarkdown(client, pageBlocks.Results, buffer, true)
	}

	return nil
}

type richText struct {
	hasAnnotations    bool
	notionAnnotations *notion.Annotations
	stringAnnotation  string
	text              string
}

// TODO: Handle annotations better
func writeRichText(client *notion.Client, buffer *bufio.Writer, richTextBlock []notion.RichText) error {
	richTexts := []richText{}

	for _, text := range richTextBlock {
		var annotation string
		r := richText{}
		b := &bytes.Buffer{}
		richTextBuffer := bufio.NewWriter(b)
		r.notionAnnotations = text.Annotations

		if hasAnnotation(text.Annotations) {
			r.hasAnnotations = true
			annotation = annotationsToStyle(text.Annotations)
			r.stringAnnotation = annotation
		}

		richTextBuffer.WriteString(annotation)

		switch text.Type {
		case notion.RichTextTypeText:
			link := text.Text.Link
			if link != nil && !strings.Contains(annotation, "`") {
				if strings.HasPrefix(link.URL, "/") {
					// Link to internal Notion page
					err := fetchPage(client, strings.TrimPrefix(link.URL, "/"), text.PlainText, richTextBuffer)
					if err != nil {
						return err
					}
				} else {
					richTextBuffer.WriteString(fmt.Sprintf("[%s](%s)", text.Text.Content, link.URL))
				}
			} else {
				richTextBuffer.WriteString(text.Text.Content)
			}
		case notion.RichTextTypeMention:
			switch text.Mention.Type {
			case notion.MentionTypePage:
				err := fetchPage(client, text.Mention.Page.ID, text.PlainText, richTextBuffer)
				if err != nil {
					return err
				}
			case notion.MentionTypeDatabase:
				value := "[[" + text.PlainText + "]]"
				richTextBuffer.WriteString(value)
			case notion.MentionTypeDate:
				value := "[[" + text.Mention.Date.Start.Format("2006-01-02") + "]]"
				richTextBuffer.WriteString(value)
			case notion.MentionTypeLinkPreview:
				richTextBuffer.WriteString(text.Mention.LinkPreview.URL)
			case notion.MentionTypeTemplateMention:
			case notion.MentionTypeUser:
			}
		case notion.RichTextTypeEquation:
			richTextBuffer.WriteString(fmt.Sprintf("$$%s$$", text.Equation.Expression))
		}

		richTextBuffer.WriteString(reverseString(annotation))

		err := richTextBuffer.Flush()
		if err != nil {
			return err
		}
		r.text = b.String()
		richTexts = append(richTexts, r)
	}

	var result string
	for i, richText := range richTexts {
		if i > 0 {
			if richText.hasAnnotations && richTexts[i-1].hasAnnotations {
				// There is a corner case for nested annotation of bold and italic
				// https://help.obsidian.md/Editing+and+formatting/Basic+formatting+syntax#Bold%2C+italics%2C+highlights
				// We only account for the easy case for now
				if (richTexts[i-1].notionAnnotations.Bold && !richTexts[i-1].notionAnnotations.Italic) && (richText.notionAnnotations.Bold && richText.notionAnnotations.Italic) {
					result = strings.TrimRight(result, "*")
					text := richText.text
					text = strings.TrimLeft(text, "*")
					text = strings.TrimRight(text, "*")
					text = "_" + text + "_**"
					result += text
				} else {
					leftAnnotations := richTexts[i-1].stringAnnotation
					rightAnnotations := richText.stringAnnotation
					result = strings.TrimRight(result, reverseString(rightAnnotations))
					test := strings.TrimLeft(richText.text, leftAnnotations)
					result += test
				}
			} else {
				result += richText.text
			}
		} else {
			result += richText.text
		}
	}

	buffer.WriteString(result)

	return nil
}

const Untitled = "Untitled"

// fetchPage check if the page has been extracted before avoiding doing to many queries to notion.so
// If the page is not stored in cached will download the page.
// If the page title has been provided it will use it, if is an empty string it will try to extracted from the page information.
// The logic to extract the title varies depending on the page parent's type.
func fetchPage(client *notion.Client, pageID, title string, buffer *bufio.Writer) error {
	val, ok := mentionCache.Get(pageID)
	if ok {
		buffer.WriteString(val)
	} else {
		if title != "" && title == Untitled {
			// Notion pages with Untitled would return a 404 when fetching them``
			// We do not process those
			mentionCache.Set(pageID, Untitled)
			return nil
		}

		// There could be pages that self reference them
		// We need a way to mark that a page is being work on
		// to avoid endless loop
		if mentionCache.IsWorking(pageID) {
			return nil
		}

		mentionCache.Mark(pageID)
		var result string
		defer mentionCache.Set(pageID, result)

		mentionPage, err := client.FindPageByID(context.Background(), pageID)
		if err != nil {
			return fmt.Errorf("failed to find page %s. error %s", pageID, err.Error())
		}

		emptyList := map[string]bool{}
		extractTitle := false
		childTitle := title
		if childTitle == "" {
			extractTitle = true
		}
		switch mentionPage.Parent.Type {
		case notion.ParentTypeDatabase:
			if extractTitle {
				props := mentionPage.Properties.(notion.DatabasePageProperties)
				childTitle = extractPlainTextFromRichText(props["Name"].Title)
			}

			var childPath string
			// Since we are migrating from the same DB we do need to create a subfolder
			// within the Obsidian vault. So we can skip fetching the database to gather
			// the name to create the subfolder
			if !empty(databaseID) && *databaseID != mentionPage.Parent.DatabaseID {
				dbPage, err := client.FindDatabaseByID(context.Background(), mentionPage.Parent.DatabaseID)
				if err != nil {
					return fmt.Errorf("failed to find parent db %s.  error: %w", mentionPage.Parent.DatabaseID, err)
				}

				dbTitle := extractPlainTextFromRichText(dbPage.Title)

				childPath = path.Join(dbTitle, fmt.Sprintf("%s.md", childTitle))
			} else {
				childPath = fmt.Sprintf("%s.md", childTitle)
			}

			if err = fetchAndSaveToObsidianVault(client, mentionPage, emptyList, path.Join(vaultDestination(), childPath), true); err != nil {
				return fmt.Errorf("failed to fetch and save mention page %s content with DB %s. error: %w", childTitle, mentionPage.Parent.DatabaseID, err)
			}
		case notion.ParentTypeBlock:
			parentPage, err := client.FindPageByID(context.Background(), mentionPage.Parent.BlockID)
			if err != nil {
				return fmt.Errorf("failed to find parent block %s.  error: %w", mentionPage.Parent.BlockID, err)
			}
			if extractTitle {
				var title []notion.RichText

				if parentPage.Parent.Type == notion.ParentTypeDatabase {
					props := parentPage.Properties.(notion.DatabasePageProperties)
					for _, val := range props {
						if val.Type == notion.DBPropTypeTitle {
							title = val.Title
							break
						}
					}
				} else {
					props := parentPage.Properties.(notion.PageProperties)
					title = props.Title.Title
				}

				childTitle = extractPlainTextFromRichText(title)
			}

			if err = fetchAndSaveToObsidianVault(client, mentionPage, emptyList, path.Join(vaultDestination(), childTitle), false); err != nil {
				return fmt.Errorf("failed to fetch and save mention page %s content with block parent %s. error: %w", childTitle, mentionPage.Parent.BlockID, err)
			}
		case notion.ParentTypePage:
			parentPage, err := client.FindPageByID(context.Background(), mentionPage.Parent.PageID)
			if err != nil {
				return fmt.Errorf("failed to find parent mention page %s.  error: %w", mentionPage.Parent.PageID, err)
			}

			if extractTitle {
				var title []notion.RichText

				if parentPage.Parent.Type == notion.ParentTypeDatabase {
					props := parentPage.Properties.(notion.DatabasePageProperties)
					for _, val := range props {
						if val.Type == notion.DBPropTypeTitle {
							title = val.Title
							break
						}
					}
				} else {
					props := parentPage.Properties.(notion.PageProperties)
					title = props.Title.Title
				}

				childTitle = extractPlainTextFromRichText(title)
			}

			if err = fetchAndSaveToObsidianVault(client, mentionPage, emptyList, path.Join(vaultDestination(), childTitle), false); err != nil {
				fmt.Printf("failed to fetch mention page content with page parent: %s\n", childTitle)
			}
		default:
			return fmt.Errorf("unsupported mention page type %s", mentionPage.Parent.Type)
		}

		if childTitle != "" {
			result = "[[" + childTitle + "]]"

			buffer.WriteString(result)
		}
	}

	return nil
}

func writeTable(client *notion.Client, tableWidth int, block notion.Block, buffer *bufio.Writer) error {
	if block.HasChildren() {
		pageBlocks, err := client.FindBlockChildrenByID(context.Background(), block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract table children blocks for block ID %s. error: %w", block.ID(), err)
		}

		for rowIndex, object := range pageBlocks.Results {
			row := object.(*notion.TableRowBlock)
			for i, cell := range row.Cells {
				if err = writeRichText(client, buffer, cell); err != nil {
					return err
				}
				buffer.WriteString("|")
				if i+1 == tableWidth {
					buffer.WriteString("\n")
					if rowIndex == 0 {
						for y := 1; y <= tableWidth; y++ {
							buffer.WriteString("--|")
						}
						buffer.WriteString("\n")
					}
				}
			}
		}
	}

	return nil
}

func annotationsToStyle(annotations *notion.Annotations) string {
	var style string
	if annotations.Bold {
		if annotations.Italic {
			style += "***"
		} else {
			style += "**"
		}
	} else {
		if annotations.Italic {
			style += "_"
		}
	}

	if annotations.Strikethrough {
		style += "~~"
	}

	if annotations.Color != notion.ColorDefault {
		style += "=="
	}

	if annotations.Code {
		style += "`"
	}

	return style
}

func hasAnnotation(annotations *notion.Annotations) bool {
	return annotations.Bold || annotations.Strikethrough || annotations.Italic || annotations.Code || annotations.Color != notion.ColorDefault
}

func reverseString(s string) string {
	rns := []rune(s) // convert to rune
	for i, j := 0, len(rns)-1; i < j; i, j = i+1, j-1 {

		// swap the letters of the string,
		// like first with last and so on.
		rns[i], rns[j] = rns[j], rns[i]
	}

	// return the reversed string.
	return string(rns)
}

func extractPlainTextFromRichText(richText []notion.RichText) string {
	buffer := new(strings.Builder)

	for _, text := range richText {
		buffer.WriteString(text.PlainText)
	}

	return buffer.String()
}
