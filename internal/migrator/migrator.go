package migrator

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GustavoCaso/n2o/internal/cache"
	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/dstotijn/go-notion"
	"github.com/itchyny/timefmt-go"
)

type image struct {
	external bool
	url      string
	name     string
}

type page struct {
	Path       string
	id         string
	buffer     *strings.Builder
	notionPage notion.Page
	coverPhoto *image
	images     []*image
	parent     *page
	children   []*page
}

type Migrator struct {
	NotionClient *notion.Client
	Config       config.Config
	Cache        *cache.Cache
	Pages        []*page
	HttpClient   *http.Client
}

func NewMigrator(config config.Config, cache *cache.Cache) *Migrator {
	notionClient := notion.NewClient(config.Token)

	return &Migrator{
		NotionClient: notionClient,
		Config:       config,
		Cache:        cache,
		HttpClient:   http.DefaultClient,
	}
}

func (m *Migrator) Setup() error {
	if m.Config.StoreImages {
		err := os.MkdirAll(m.Config.VaultImagePath(), 0770)
		if err != nil {
			return fmt.Errorf("failed to create image folder. error: %s", err.Error())
		}
	}
	return nil
}

func (m *Migrator) FetchPages(ctx context.Context) error {
	if m.Config.DatabaseID != "" {
		db, err := m.NotionClient.FindDatabaseByID(ctx, m.Config.DatabaseID)
		if err != nil {
			return fmt.Errorf("failed to get DB %s. error: %s\n", m.Config.DatabaseID, err.Error())
		}
		dbTitle := extractPlainTextFromRichText(db.Title)
		notionPages, err := m.fetchNotionDBPages(ctx)
		if err != nil {
			return fmt.Errorf("failed to get pages from DB %s. error: %s\n", m.Config.DatabaseID, err.Error())
		}
		pages := make([]*page, len(notionPages))

		for i, notionPage := range notionPages {
			page := &page{
				id:         notionPage.ID,
				buffer:     &strings.Builder{},
				Path:       path.Join(m.Config.VaultFilepath(), dbTitle, m.extractPageTitle(notionPage)),
				notionPage: notionPage,
				parent:     nil,
			}

			if notionPage.Cover != nil {
				cover := &image{
					external: true,
					url:      notionPage.Cover.External.URL,
				}

				page.coverPhoto = cover
			}

			pages[i] = page
		}

		m.Pages = pages

		return nil
	} else {
		notionPage, err := m.NotionClient.FindPageByID(context.Background(), m.Config.PageID)
		if err != nil {
			return fmt.Errorf("failed to find the page %s make sure the page exists in your Notioin workspace. error: %s\n", m.Config.PageID, err.Error())
		}

		var cover *image
		if notionPage.Cover != nil {
			cover = &image{
				external: true,
				url:      notionPage.Cover.External.URL,
			}
		}

		m.Pages = []*page{
			{
				id:         notionPage.ID,
				buffer:     &strings.Builder{},
				Path:       path.Join(m.Config.VaultFilepath(), m.extractPageTitle(notionPage)),
				notionPage: notionPage,
				parent:     nil,
				coverPhoto: cover,
			},
		}

		return nil
	}
}

func (m *Migrator) extractPageTitle(page notion.Page) string {
	var str string

	switch page.Parent.Type {
	case notion.ParentTypeDatabase:
		properties := page.Properties.(notion.DatabasePageProperties)
		sortedPropkeys := make([]string, 0, len(properties))

		for k := range properties {
			sortedPropkeys = append(sortedPropkeys, k)
		}

		sort.Strings(sortedPropkeys)

		var titleProperty notion.DatabasePageProperty

		for _, key := range sortedPropkeys {
			value := properties[key]

			if value.Type == notion.DBPropTypeTitle {
				titleProperty = value
			}

			val, ok := m.Config.PageNameFilters[strings.ToLower(key)]

			if ok {
				switch value.Type {
				case notion.DBPropTypeDate:
					date := value.Date.Start
					if val != "" {
						str += timefmt.Format(date.Time, val)
					} else {
						str += date.Time.String()
					}
				case notion.DBPropTypeTitle:
					str += extractPlainTextFromRichText(value.Title)
				default:
					fmt.Printf("type: `%s` for extracting page title not supported\n", value.Type)
				}
			}
		}

		// In the case we did not find any element to create the page title, we default to the title property
		if str == "" {
			str = extractPlainTextFromRichText(titleProperty.Title)
		}
	case notion.ParentTypeWorkspace:
		fallthrough
	case notion.ParentTypeBlock:
		fallthrough
	case notion.ParentTypePage:
		properties := page.Properties.(notion.PageProperties)
		str = extractPlainTextFromRichText(properties.Title.Title)
	}

	fileName := fmt.Sprintf("%s.md", str)
	return fileName
}

func (m *Migrator) FetchParseAndSavePage(ctx context.Context, page *page, pageProperties map[string]bool) error {
	return m.fetchParseAndSavePage(ctx, page, pageProperties)
}

func (m *Migrator) fetchParseAndSavePage(ctx context.Context, page *page, pageProperties map[string]bool) error {
	pageBlocks, err := m.NotionClient.FindBlockChildrenByID(ctx, page.notionPage.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", page.notionPage.ID, err)
	}

	if page.notionPage.Parent.Type == notion.ParentTypeDatabase && len(pageProperties) > 0 {
		props := page.notionPage.Properties.(notion.DatabasePageProperties)

		frotmatterProps := make(notion.DatabasePageProperties)

		allProps := false

		if pageProperties["all"] {
			allProps = true
		}

		sortedPropkeys := make([]string, 0, len(props))

		for k := range props {
			sortedPropkeys = append(sortedPropkeys, k)
		}

		sort.Strings(sortedPropkeys)

		for _, propKey := range sortedPropkeys {
			propValue := props[propKey]
			if pageProperties[strings.ToLower(propKey)] || allProps {
				frotmatterProps[propKey] = propValue
			}
		}

		if len(frotmatterProps) > 0 {
			m.propertiesToFrontMatter(ctx, page, sortedPropkeys, frotmatterProps, page.buffer)
		}
	}

	if page.coverPhoto != nil {
		page.buffer.WriteString(fmt.Sprintf("![700x200](%s)", page.coverPhoto.url))
		page.buffer.WriteString("\n\n")
	}

	err = m.pageToMarkdown(ctx, page, pageBlocks.Results, false)

	if err != nil {
		return fmt.Errorf("failed to convert page to markdown. error: %w", err)
	}

	return nil
}

func (m *Migrator) DisplayInformation(ctx context.Context) error {
	buffer := bufio.NewWriter(os.Stdout)
	for _, page := range m.Pages {
		buffer.WriteString(fmt.Sprintf("%s \n", m.removeObsidianVault(page.Path)))
		m.displayPageInfo(page, buffer, 0)
	}

	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("failed to write into stdout. error: %w", err)
	}

	return nil
}

func (m *Migrator) displayPageInfo(page *page, buffer *bufio.Writer, index int) {
	var spaces int
	if index > 0 {
		spaces = index * 5
	} else {
		spaces = 4
	}
	for _, childPage := range page.children {
		buffer.WriteString(fmt.Sprintf("%*s %s\n", spaces, "|->", m.removeObsidianVault(childPage.Path)))
		m.displayPageInfo(childPage, buffer, 2)
	}
}

func (m *Migrator) removeObsidianVault(s string) string {
	return strings.TrimPrefix(s, m.Config.VaultPath)
}

func (m *Migrator) WritePagesToDisk(ctx context.Context) error {
	// err := m.Setup()
	// if err != nil {
	// 	return err
	// }

	for _, page := range m.Pages {
		err := m.writePage(page)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Migrator) writePage(page *page) error {
	if err := os.MkdirAll(filepath.Dir(page.Path), 0770); err != nil {
		return fmt.Errorf("failed to create the necessary directories in for the Obsidian vault.  error: %w", err)
	}

	f, err := os.Create(page.Path)
	if err != nil {
		return fmt.Errorf("failed to create the markdown file %s. error: %w", path.Base(page.Path), err)
	}

	defer f.Close()

	output := page.buffer.String()

	_, err = f.WriteString(output)
	if err != nil {
		return err
	}

	if m.Config.StoreImages {
		for _, image := range page.images {
			err := m.downloadImage(image.name, image.url)
			return err
		}
	}

	for _, childPage := range page.children {
		childErr := m.writePage(childPage)
		if childErr != nil {
			return childErr
		}
	}

	return nil
}

func (m *Migrator) propertiesToFrontMatter(ctx context.Context, parentPage *page, sortedKeys []string, propertites notion.DatabasePageProperties, buffer *strings.Builder) {
	buffer.WriteString("---\n")
	// There is a limitation between Notions and Obsidian.
	// If the property is named tags in Notion it has ramifications in Obsidian
	// For example Notion relation property name tags would break in Obsidian
	// Workaround rename the Notion property to "Related to tags"
	for _, key := range sortedKeys {
		value := propertites[key]
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
			// TODO: Needs to handle relations bigger then 25
			// https://developers.notion.com/reference/retrieve-a-page-property
			b := &strings.Builder{}
			for i, relation := range value.Relation {
				if i == 0 {
					b.WriteString("\n  - ")
				} else {
					b.WriteString("  - ")
				}

				err := m.fetchPage(ctx, parentPage, relation.ID, "", b, true)
				if err != nil {
					// We do not want to break the migration proccess for this case
					fmt.Println("failed to get page relation for frontmatter")
					continue
				}
				b.WriteString("\n")
			}
			buffer.WriteString(fmt.Sprintf("%s: %s\n", key, b.String()))
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
			case notion.RollupResultTypeArray:
				b := &bytes.Buffer{}
				rollupBuffer := bufio.NewWriter(b)
				rollupBuffer.WriteString(fmt.Sprintf("%s: ", key))

				numbers := []float64{}
				for _, prop := range value.Rollup.Array {
					if prop.Type == notion.DBPropTypeNumber {
						numbers = append(numbers, *prop.Number)
					}
				}

				rollupBuffer.WriteString(fmt.Sprintf("%f\n", numbers))
				rollupBuffer.Flush()
				buffer.WriteString(b.String())
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

const Untitled = "Untitled"

// fetchPage check if the page has been extracted before avoiding doing to many queries to notion.so
// If the page is not stored in cached will download the page.
// If the page title has been provided it will use it, if is an empty string it will try to extracted from the page information.
// The logic to extract the title varies depending on the page parent's type.
// TODO: handle better the quote logic
func (m *Migrator) fetchPage(ctx context.Context, parentPage *page, pageID, title string, buffer *strings.Builder, quotes bool) error {
	cached, ok := m.Cache.Get(pageID)
	if ok {
		var result string
		if quotes {
			result = fmt.Sprintf("\"[[%s]]\"", cached.Title)
		} else {
			result = fmt.Sprintf("[[%s]]", cached.Title)
		}
		buffer.WriteString(result)
		return nil
	} else {
		if title != "" && title == Untitled {
			// Notion pages with Untitled would return a 404 when fetching them
			// We do not process those
			m.Cache.Set(pageID, cache.Page{
				Title: Untitled,
			})
			return nil
		}

		// There could be pages that self reference them
		// We need a way to mark that a page is being work on
		// to avoid endless loop. In this case we just want to get the page title
		// TODO: Check if we need this logic
		if m.Cache.IsWorking(pageID) {
			childTitle := title
			extractTitle := false
			if childTitle == "" {
				extractTitle = true
			}
			mentionPage, err := m.NotionClient.FindPageByID(ctx, pageID)
			if err != nil {
				return fmt.Errorf("failed to find page %s. error %s", pageID, err.Error())
			}

			switch mentionPage.Parent.Type {
			case notion.ParentTypeDatabase:
				if extractTitle {
					childTitle = m.extractPageTitle(mentionPage)
				}

				// Since we are migrating from the same DB we do need to create a subfolder
				// within the Obsidian vault. So we can skip fetching the database to gather
				// the name to create the subfolder
				if m.Config.DatabaseID == "" || m.Config.DatabaseID != mentionPage.Parent.DatabaseID {
					dbPage, err := m.NotionClient.FindDatabaseByID(ctx, mentionPage.Parent.DatabaseID)
					if err != nil {
						return fmt.Errorf("failed to find parent db %s.  error: %w", mentionPage.Parent.DatabaseID, err)
					}

					dbTitle := extractPlainTextFromRichText(dbPage.Title)

					childTitle = path.Join(dbTitle, childTitle)
				}
			case notion.ParentTypeBlock:
				notionParentPage, err := m.NotionClient.FindPageByID(ctx, mentionPage.Parent.BlockID)
				if err != nil {
					return fmt.Errorf("failed to find parent block %s.  error: %w", mentionPage.Parent.BlockID, err)
				}

				if extractTitle {
					childTitle = m.extractPageTitle(notionParentPage)
				}

			case notion.ParentTypePage:
				notionParentPage, err := m.NotionClient.FindPageByID(ctx, mentionPage.Parent.PageID)
				if err != nil {
					return fmt.Errorf("failed to find parent mention page %s.  error: %w", mentionPage.Parent.PageID, err)
				}

				if extractTitle {
					childTitle = m.extractPageTitle(notionParentPage)
				}
			default:
				return fmt.Errorf("unsupported mention page type %s", mentionPage.Parent.Type)
			}

			var result string
			if quotes {
				result = fmt.Sprintf("\"[[%s]]\"", childTitle)
			} else {
				result = fmt.Sprintf("[[%s]]", childTitle)
			}
			buffer.WriteString(result)
			return nil
		}

		m.Cache.Mark(pageID)
		var result string

		extractTitle := false
		childTitle := title
		if childTitle == "" {
			extractTitle = true
		} else {
			if !strings.HasSuffix(childTitle, ".md") {
				childTitle = fmt.Sprintf("%s.md", childTitle)
			}
		}

		defer func() {
			m.Cache.Set(pageID, cache.Page{
				Title: childTitle,
			})
		}()

		mentionPage, err := m.NotionClient.FindPageByID(ctx, pageID)
		if err != nil {
			return fmt.Errorf("failed to find page %s. error %s", pageID, err.Error())
		}

		switch mentionPage.Parent.Type {
		case notion.ParentTypeDatabase:
			if extractTitle {
				childTitle = m.extractPageTitle(mentionPage)
			}
			// Since we are migrating from the same DB we do need to create a subfolder
			// within the Obsidian vault. So we can skip fetching the database to gather
			// the name to create the subfolder
			if m.Config.DatabaseID == "" || m.Config.DatabaseID != mentionPage.Parent.DatabaseID {
				dbPage, err := m.NotionClient.FindDatabaseByID(ctx, mentionPage.Parent.DatabaseID)
				if err != nil {
					return fmt.Errorf("failed to find parent db %s.  error: %w", mentionPage.Parent.DatabaseID, err)
				}

				dbTitle := extractPlainTextFromRichText(dbPage.Title)

				childTitle = path.Join(dbTitle, childTitle)
			}
		case notion.ParentTypeBlock:
			notionParentPage, err := m.NotionClient.FindPageByID(ctx, mentionPage.Parent.BlockID)
			if err != nil {
				return fmt.Errorf("failed to find parent block %s.  error: %w", mentionPage.Parent.BlockID, err)
			}
			if extractTitle {
				childTitle = m.extractPageTitle(notionParentPage)
			}
		case notion.ParentTypePage:
			notionParentPage, err := m.NotionClient.FindPageByID(ctx, mentionPage.Parent.PageID)
			if err != nil {
				return fmt.Errorf("failed to find parent mention page %s.  error: %w", mentionPage.Parent.PageID, err)
			}

			if extractTitle {
				childTitle = m.extractPageTitle(notionParentPage)
			}
		default:
			return fmt.Errorf("unsupported mention page type %s", mentionPage.Parent.Type)
		}

		if childTitle == "" {
			return fmt.Errorf("unable to find page information %s", pageID)
		}

		newPage := &page{
			id:         mentionPage.ID,
			notionPage: mentionPage,
			buffer:     &strings.Builder{},
			parent:     parentPage,
			Path:       path.Join(m.Config.VaultFilepath(), childTitle),
		}

		if mentionPage.Cover != nil {
			newPage.coverPhoto = &image{
				external: true,
				url:      mentionPage.Cover.External.URL,
			}
		}

		parentPage.children = append(parentPage.children, newPage)

		if err = m.fetchParseAndSavePage(ctx, newPage, m.Config.PagePropertiesToMigrate); err != nil {
			fmt.Printf("failed to fetch mention page content with page parent: %s\n", childTitle)
		}

		if quotes {
			result = fmt.Sprintf("\"[[%s]]\"", childTitle)
		} else {
			result = fmt.Sprintf("[[%s]]", childTitle)
		}

		buffer.WriteString(result)

		return nil
	}

}

func (m *Migrator) pageToMarkdown(ctx context.Context, parentPage *page, blocks []notion.Block, indent bool) error {
	var err error
	buffer := parentPage.buffer

	for _, object := range blocks {
		switch block := object.(type) {
		case *notion.Heading1Block:
			if indent {
				buffer.WriteString("	# ")
			} else {
				buffer.WriteString("# ")
			}
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.Heading2Block:
			if indent {
				buffer.WriteString("	## ")
			} else {
				buffer.WriteString("## ")
			}
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.Heading3Block:
			if indent {
				buffer.WriteString("	### ")
			} else {
				buffer.WriteString("### ")
			}
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
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
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.ParagraphBlock:
			if len(block.RichText) > 0 {
				if indent {
					buffer.WriteString("	")
					if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
						return err
					}
				} else {
					if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
						return err
					}
				}
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.BulletedListItemBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.NumberedListItemBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
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
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
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
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.QuoteBlock:
			if indent {
				buffer.WriteString("	> ")
			} else {
				buffer.WriteString("> ")
			}
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
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
			err := m.fetchPage(ctx, parentPage, block.PageID, "", buffer, false)
			if err != nil {
				return err
			}
			buffer.WriteString("\n")
		case *notion.CodeBlock:
			buffer.WriteString("```")
			buffer.WriteString(*block.Language)
			buffer.WriteString("\n")
			if err = m.writeRichText(ctx, parentPage, buffer, block.RichText); err != nil {
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
			if block.Type == notion.FileTypeFile && m.Config.StoreImages {
				imageName := filepath.Join(m.removeObsidianVault(parentPage.Path), block.ID()+".png")

				parentPage.images = append(parentPage.images, &image{
					external: false,
					url:      block.File.URL,
					name:     imageName,
				})

				if indent {
					buffer.WriteString(fmt.Sprintf("	![[%s]]", filepath.Join("Images", imageName)))
				} else {
					buffer.WriteString(fmt.Sprintf("![[%s]]", filepath.Join("Images", imageName)))
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
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.ColumnBlock:
			if err = m.writeChrildren(ctx, parentPage, object); err != nil {
				return err
			}
		case *notion.TableBlock:
			if err = m.writeTable(ctx, parentPage, block.TableWidth, object, buffer); err != nil {
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

type richText struct {
	hasAnnotations    bool
	notionAnnotations *notion.Annotations
	stringAnnotation  string
	text              string
}

// TODO: Handle annotations better
func (m *Migrator) writeRichText(ctx context.Context, parentPage *page, buffer *strings.Builder, richTextBlock []notion.RichText) error {
	richTexts := []richText{}

	for _, text := range richTextBlock {
		var annotation string
		r := richText{}

		richTextBuffer := &strings.Builder{}
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
					err := m.fetchPage(ctx, parentPage, strings.TrimPrefix(link.URL, "/"), text.PlainText, richTextBuffer, false)
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
				err := m.fetchPage(ctx, parentPage, text.Mention.Page.ID, text.PlainText, richTextBuffer, false)
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

		r.text = richTextBuffer.String()
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

	parentPage.buffer.WriteString(result)

	return nil
}

func (m *Migrator) writeChrildren(ctx context.Context, parentPage *page, block notion.Block) error {
	if block.HasChildren() {
		pageBlocks, err := m.NotionClient.FindBlockChildrenByID(ctx, block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", block.ID(), err)
		}
		return m.pageToMarkdown(ctx, parentPage, pageBlocks.Results, true)
	}

	return nil
}

func (m *Migrator) writeTable(ctx context.Context, parentPage *page, tableWidth int, block notion.Block, buffer *strings.Builder) error {
	if block.HasChildren() {
		pageBlocks, err := m.NotionClient.FindBlockChildrenByID(ctx, block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract table children blocks for block ID %s. error: %w", block.ID(), err)
		}

		for rowIndex, object := range pageBlocks.Results {
			row := object.(*notion.TableRowBlock)
			for i, cell := range row.Cells {
				if err = m.writeRichText(ctx, parentPage, buffer, cell); err != nil {
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

func (m *Migrator) fetchNotionDBPages(ctx context.Context) ([]notion.Page, error) {
	notionResponse, err := m.NotionClient.QueryDatabase(ctx, m.Config.DatabaseID, nil)
	if err != nil {
		return []notion.Page{}, err
	}

	result := []notion.Page{}

	result = append(result, notionResponse.Results...)

	query := &notion.DatabaseQuery{}
	for notionResponse.HasMore {
		query.StartCursor = *notionResponse.NextCursor

		notionResponse, err = m.NotionClient.QueryDatabase(ctx, m.Config.DatabaseID, query)
		if err != nil {
			return []notion.Page{}, err
		}

		result = append(result, notionResponse.Results...)
	}

	return result, nil
}

func (m *Migrator) downloadImage(name, url string) error {
	imageLocation := filepath.Join(m.Config.VaultImagePath(), name)
	if err := os.MkdirAll(path.Dir(imageLocation), 0770); err != nil {
		return fmt.Errorf("failed to create the necessary directories to store images.  error: %w", err)
	}

	response, err := m.HttpClient.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// We should not worry about checking if the file exists
	// it has been created by the caller when calling Migrator.Setup()
	file, err := os.Create(imageLocation)
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
