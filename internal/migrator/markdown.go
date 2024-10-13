package migrator

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dstotijn/go-notion"
)

func (m *migrator) propertiesToFrontMatter(ctx context.Context, parentPage *Page, sortedKeys []string, propertites notion.DatabasePageProperties, buffer *strings.Builder) {
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
					m.logger.Info("failed to get page relation for frontmatter")
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

func (m *migrator) pageToMarkdown(ctx context.Context, parentPage *Page, blocks []notion.Block, indent bool) error {
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
			if block.Type == notion.FileTypeFile && m.config.StoreImages {
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
			m.logger.Warn(fmt.Sprintf("Child database `%s` found on page `%s`. You might want to migrate that database separately", block.Title, m.removeObsidianVault(parentPage.Path)))

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
func (m *migrator) writeRichText(ctx context.Context, parentPage *Page, buffer *strings.Builder, richTextBlock []notion.RichText) error {
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

func (m *migrator) writeChrildren(ctx context.Context, parentPage *Page, block notion.Block) error {
	if block.HasChildren() {
		pageBlocks, err := m.notionClient.FindBlockChildrenByID(ctx, block.ID(), nil)
		if err != nil {
			return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", block.ID(), err)
		}
		return m.pageToMarkdown(ctx, parentPage, pageBlocks.Results, true)
	}

	return nil
}

func (m *migrator) writeTable(ctx context.Context, parentPage *Page, tableWidth int, block notion.Block, buffer *strings.Builder) error {
	if block.HasChildren() {
		pageBlocks, err := m.notionClient.FindBlockChildrenByID(ctx, block.ID(), nil)
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
