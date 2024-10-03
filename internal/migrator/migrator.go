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

type Migrator struct {
	Client *notion.Client
	Config config.Config
	Cache  *cache.Cache
}

func NewMigrator(config config.Config, cache *cache.Cache) Migrator {
	client := notion.NewClient(config.Token)

	return Migrator{
		Client: client,
		Config: config,
		Cache:  cache,
	}
}

func (m Migrator) Setup() error {
	if m.Config.StoreImages {
		err := os.MkdirAll(m.Config.VaultFilepath(), 0770)
		if err != nil {
			return fmt.Errorf("failed to create image folder. error: %s", err.Error())
		}
	}
	return nil
}

func (m Migrator) FetchPages(ctx context.Context) ([]notion.Page, error) {
	if m.Config.DatabaseID != "" {
		pages, err := m.fetchNotionDBPages(ctx)
		if err != nil {
			return []notion.Page{}, fmt.Errorf("failed to gets all pages from DB %s. error: %s\n", m.Config.DatabaseID, err.Error())
		}

		return pages, nil
	} else {
		page, err := m.Client.FindPageByID(context.Background(), m.Config.PageID)
		if err != nil {
			return []notion.Page{}, fmt.Errorf("failed to find the page %s make sure the page exists in your Notioin workspace. error: %s\n", m.Config.PageID, err.Error())
		}

		return []notion.Page{page}, nil
	}
}

func (m Migrator) ExtractPageTitle(page notion.Page) string {
	var str string

	if m.Config.DatabaseID != "" {
		properties := page.Properties.(notion.DatabasePageProperties)
		sortedPropkeys := make([]string, 0, len(properties))

		for k := range properties {
			sortedPropkeys = append(sortedPropkeys, k)
		}

		sort.Strings(sortedPropkeys)

		for _, key := range sortedPropkeys {
			value := properties[key]
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
	} else {
		properties := page.Properties.(notion.PageProperties)
		str = extractPlainTextFromRichText(properties.Title.Title)
	}

	fileName := fmt.Sprintf("%s.md", str)
	return path.Join(m.Config.VaultFilepath(), fileName)
}

func (m Migrator) FetchParseAndSavePage(ctx context.Context, page notion.Page, pageProperties map[string]bool, storePath string) error {
	pageBlocks, err := m.Client.FindBlockChildrenByID(ctx, page.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", page.ID, err)
	}

	if err := os.MkdirAll(filepath.Dir(storePath), 0770); err != nil {
		return fmt.Errorf("failed to create the necessary directories in for the Obsidian vault.  error: %w", err)
	}

	f, err := os.Create(storePath)
	if err != nil {
		return fmt.Errorf("failed to create the markdown file %s. error: %w", path.Base(storePath), err)
	}

	defer f.Close()

	// create new buffer
	buffer := bufio.NewWriter(f)

	if page.Parent.Type == notion.ParentTypeDatabase && len(pageProperties) > 0 {
		props := page.Properties.(notion.DatabasePageProperties)

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
			m.propertiesToFrontMatter(ctx, sortedPropkeys, frotmatterProps, buffer)
		}
	}

	err = m.pageToMarkdown(ctx, pageBlocks.Results, buffer, false)

	if err != nil {
		return fmt.Errorf("failed to convert page to markdown. error: %w", err)
	}

	if err = buffer.Flush(); err != nil {
		return fmt.Errorf("failed to write into the markdown file %s. error: %w", path.Base(m.Config.VaultPath), err)
	}

	return nil
}

func (m Migrator) FetchAndDisplayInformation(ctx context.Context, page notion.Page, storePath string) error {
	pageBlocks, err := m.Client.FindBlockChildrenByID(ctx, page.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", page.ID, err)
	}

	buffer := bufio.NewWriter(&strings.Builder{})

	// err = m.pageToMarkdown(ctx, pageBlocks.Results, buffer, false)

	// if err != nil {
	// 	return fmt.Errorf("failed to convert page to markdown. error: %w", err)
	// }

	// if err = buffer.Flush(); err != nil {
	// 	return fmt.Errorf("failed to write into the markdown file %s. error: %w", path.Base(m.Config.VaultPath), err)
	// }

	return nil
}

func (m Migrator) propertiesToFrontMatter(ctx context.Context, sortedKeys []string, propertites notion.DatabasePageProperties, buffer *bufio.Writer) {
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
			b := &bytes.Buffer{}
			relationBuffer := bufio.NewWriter(b)
			for i, relation := range value.Relation {
				if i == 0 {
					relationBuffer.WriteString("\n  - ")
				} else {
					relationBuffer.WriteString("  - ")
				}

				err := m.fetchPage(ctx, relation.ID, "", relationBuffer, true)
				if err != nil {
					// We do not want to break the migration proccess for this case
					fmt.Println("failed to get page relation for frontmatter")
					continue
				}
				relationBuffer.WriteString("\n")
			}
			relationBuffer.Flush()
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
func (m Migrator) fetchPage(ctx context.Context, pageID, title string, buffer *bufio.Writer, quotes bool) error {
	val, ok := m.Cache.Get(pageID)
	if ok {
		buffer.WriteString(val)
	} else {
		if title != "" && title == Untitled {
			// Notion pages with Untitled would return a 404 when fetching them
			// We do not process those
			m.Cache.Set(pageID, Untitled)
			return nil
		}

		// There could be pages that self reference them
		// We need a way to mark that a page is being work on
		// to avoid endless loop
		if m.Cache.IsWorking(pageID) {
			return nil
		}

		m.Cache.Mark(pageID)
		var result string
		defer m.Cache.Set(pageID, result)

		mentionPage, err := m.Client.FindPageByID(ctx, pageID)
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
			if m.Config.DatabaseID == "" || m.Config.DatabaseID != mentionPage.Parent.DatabaseID {
				dbPage, err := m.Client.FindDatabaseByID(ctx, mentionPage.Parent.DatabaseID)
				if err != nil {
					return fmt.Errorf("failed to find parent db %s.  error: %w", mentionPage.Parent.DatabaseID, err)
				}

				dbTitle := extractPlainTextFromRichText(dbPage.Title)

				childPath = path.Join(dbTitle, fmt.Sprintf("%s.md", childTitle))
				childTitle = path.Join(dbTitle, childTitle)
			} else {
				childPath = fmt.Sprintf("%s.md", childTitle)
			}

			if err = m.FetchParseAndSavePage(ctx, mentionPage, emptyList, path.Join(m.Config.VaultFilepath(), childPath)); err != nil {
				return fmt.Errorf("failed to fetch and save mention page %s content with DB %s. error: %w", childTitle, mentionPage.Parent.DatabaseID, err)
			}
		case notion.ParentTypeBlock:
			parentPage, err := m.Client.FindPageByID(ctx, mentionPage.Parent.BlockID)
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

			if err = m.FetchParseAndSavePage(ctx, mentionPage, emptyList, path.Join(m.Config.VaultFilepath(), childTitle)); err != nil {
				return fmt.Errorf("failed to fetch and save mention page %s content with block parent %s. error: %w", childTitle, mentionPage.Parent.BlockID, err)
			}
		case notion.ParentTypePage:
			parentPage, err := m.Client.FindPageByID(ctx, mentionPage.Parent.PageID)
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

			if err = m.FetchParseAndSavePage(ctx, mentionPage, emptyList, path.Join(m.Config.VaultFilepath(), childTitle)); err != nil {
				fmt.Printf("failed to fetch mention page content with page parent: %s\n", childTitle)
			}
		default:
			return fmt.Errorf("unsupported mention page type %s", mentionPage.Parent.Type)
		}

		if childTitle != "" {
			var result string
			if quotes {
				result = fmt.Sprintf("\"[[%s]]\"", childTitle)
			} else {
				result = fmt.Sprintf("[[%s]]", childTitle)
			}

			buffer.WriteString(result)
		}
	}

	return nil
}

func (m Migrator) pageToMarkdown(ctx context.Context, blocks []notion.Block, buffer *bufio.Writer, indent bool) error {
	var err error

	for _, object := range blocks {
		switch block := object.(type) {
		case *notion.Heading1Block:
			if indent {
				buffer.WriteString("	# ")
			} else {
				buffer.WriteString("# ")
			}
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.Heading2Block:
			if indent {
				buffer.WriteString("	## ")
			} else {
				buffer.WriteString("## ")
			}
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.Heading3Block:
			if indent {
				buffer.WriteString("	### ")
			} else {
				buffer.WriteString("### ")
			}
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
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
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.ParagraphBlock:
			if len(block.RichText) > 0 {
				if indent {
					buffer.WriteString("	")
					if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
						return err
					}
				} else {
					if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
						return err
					}
				}
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.BulletedListItemBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.NumberedListItemBlock:
			if indent {
				buffer.WriteString("	- ")
			} else {
				buffer.WriteString("- ")
			}
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
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
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
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
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.QuoteBlock:
			if indent {
				buffer.WriteString("	> ")
			} else {
				buffer.WriteString("> ")
			}
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
				return err
			}
			buffer.WriteString("\n")
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
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
			err := m.fetchPage(ctx, block.PageID, "", buffer, false)
			if err != nil {
				return err
			}
			buffer.WriteString("\n")
		case *notion.CodeBlock:
			buffer.WriteString("```")
			buffer.WriteString(*block.Language)
			buffer.WriteString("\n")
			if err = m.writeRichText(ctx, buffer, block.RichText); err != nil {
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
				name := block.ID() + ".png"
				err := m.downloadImage(name, block.File.URL)

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
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.ColumnBlock:
			if err = m.writeChrildren(ctx, object, buffer); err != nil {
				return err
			}
		case *notion.TableBlock:
			if err = m.writeTable(ctx, block.TableWidth, object, buffer); err != nil {
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
func (m Migrator) writeRichText(ctx context.Context, buffer *bufio.Writer, richTextBlock []notion.RichText) error {
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
					err := m.fetchPage(ctx, strings.TrimPrefix(link.URL, "/"), text.PlainText, richTextBuffer, false)
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
				err := m.fetchPage(ctx, text.Mention.Page.ID, text.PlainText, richTextBuffer, false)
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

func (m Migrator) writeChrildren(ctx context.Context, block notion.Block, buffer *bufio.Writer) error {
	if block.HasChildren() {
		pageBlocks, err := m.Client.FindBlockChildrenByID(ctx, block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", block.ID(), err)
		}
		return m.pageToMarkdown(ctx, pageBlocks.Results, buffer, true)
	}

	return nil
}

func (m Migrator) writeTable(ctx context.Context, tableWidth int, block notion.Block, buffer *bufio.Writer) error {
	if block.HasChildren() {
		pageBlocks, err := m.Client.FindBlockChildrenByID(ctx, block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract table children blocks for block ID %s. error: %w", block.ID(), err)
		}

		for rowIndex, object := range pageBlocks.Results {
			row := object.(*notion.TableRowBlock)
			for i, cell := range row.Cells {
				if err = m.writeRichText(ctx, buffer, cell); err != nil {
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

func (m Migrator) fetchNotionDBPages(ctx context.Context) ([]notion.Page, error) {
	notionResponse, err := m.Client.QueryDatabase(ctx, m.Config.DatabaseID, nil)
	if err != nil {
		return []notion.Page{}, err
	}

	result := []notion.Page{}

	result = append(result, notionResponse.Results...)

	query := &notion.DatabaseQuery{}
	for notionResponse.HasMore {
		query.StartCursor = *notionResponse.NextCursor

		notionResponse, err = m.Client.QueryDatabase(ctx, m.Config.DatabaseID, query)
		if err != nil {
			return []notion.Page{}, err
		}

		result = append(result, notionResponse.Results...)
	}

	return result, nil
}

func (m Migrator) downloadImage(name, url string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// We should not worry about checking if the file exists
	// it has been created by the caller when calling Migrator.Setup()
	file, err := os.Create(filepath.Join(m.Config.VaultImagePath(), name))
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
