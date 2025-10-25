package migrator

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/GustavoCaso/n2o/internal/log"
	"github.com/dstotijn/go-notion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteIndent(t *testing.T) {
	tests := []struct {
		name     string
		indent   bool
		content  string
		expected string
	}{
		{
			name:     "with indent",
			indent:   true,
			content:  "test content",
			expected: "\ttest content",
		},
		{
			name:     "without indent",
			indent:   false,
			content:  "test content",
			expected: "test content",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buffer := &strings.Builder{}
			writeIndent(buffer, test.indent, test.content)
			assert.Equal(t, test.expected, buffer.String())
		})
	}
}

func TestMarkdownImage(t *testing.T) {
	url := "https://example.com/image.png"
	result := markdownImage(url)
	assert.Equal(t, "![](https://example.com/image.png)", result)
}

func TestObsidianImage(t *testing.T) {
	imagePath := "Images/test.png"
	result := obsidianImage(imagePath)
	assert.Equal(t, "![[Images/test.png]]", result)
}

func TestFormatFrontmatterValue(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    *string
		expected string
	}{
		{
			name:     "with value",
			key:      "URL",
			value:    stringPtr("https://example.com"),
			expected: "URL: https://example.com\n",
		},
		{
			name:     "with nil value",
			key:      "URL",
			value:    nil,
			expected: "URL: \n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := formatFrontmatterValue(test.key, test.value)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestFormatFrontmatterNumber(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    *float64
		expected string
	}{
		{
			name:     "with value",
			key:      "Age",
			value:    float64Ptr(42.5),
			expected: "Age: 42.500000\n",
		},
		{
			name:     "with nil value",
			key:      "Age",
			value:    nil,
			expected: "Age: \n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := formatFrontmatterNumber(test.key, test.value)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestFormatFrontmatterBool(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    *bool
		expected string
	}{
		{
			name:     "with true value",
			key:      "Completed",
			value:    boolPtr(true),
			expected: "Completed: true\n",
		},
		{
			name:     "with false value",
			key:      "Completed",
			value:    boolPtr(false),
			expected: "Completed: false\n",
		},
		{
			name:     "with nil value",
			key:      "Completed",
			value:    nil,
			expected: "Completed: \n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := formatFrontmatterBool(test.key, test.value)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestAnnotationsToStyle(t *testing.T) {
	tests := []struct {
		name        string
		annotations *notion.Annotations
		expected    string
	}{
		{
			name: "bold",
			annotations: &notion.Annotations{
				Bold:  true,
				Color: notion.ColorDefault,
			},
			expected: "**",
		},
		{
			name: "italic",
			annotations: &notion.Annotations{
				Italic: true,
				Color:  notion.ColorDefault,
			},
			expected: "_",
		},
		{
			name: "bold and italic",
			annotations: &notion.Annotations{
				Bold:   true,
				Italic: true,
				Color:  notion.ColorDefault,
			},
			expected: "***",
		},
		{
			name: "strikethrough",
			annotations: &notion.Annotations{
				Strikethrough: true,
				Color:         notion.ColorDefault,
			},
			expected: "~~",
		},
		{
			name: "code",
			annotations: &notion.Annotations{
				Code:  true,
				Color: notion.ColorDefault,
			},
			expected: "`",
		},
		{
			name: "colored",
			annotations: &notion.Annotations{
				Color: notion.ColorBlue,
			},
			expected: "==",
		},
		{
			name: "bold, strikethrough, and code",
			annotations: &notion.Annotations{
				Bold:          true,
				Strikethrough: true,
				Code:          true,
				Color:         notion.ColorDefault,
			},
			expected: "**~~`",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := annotationsToStyle(test.annotations)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestHasAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations *notion.Annotations
		expected    bool
	}{
		{
			name: "has bold",
			annotations: &notion.Annotations{
				Bold:  true,
				Color: notion.ColorDefault,
			},
			expected: true,
		},
		{
			name: "has italic",
			annotations: &notion.Annotations{
				Italic: true,
				Color:  notion.ColorDefault,
			},
			expected: true,
		},
		{
			name: "has strikethrough",
			annotations: &notion.Annotations{
				Strikethrough: true,
				Color:         notion.ColorDefault,
			},
			expected: true,
		},
		{
			name: "has code",
			annotations: &notion.Annotations{
				Code:  true,
				Color: notion.ColorDefault,
			},
			expected: true,
		},
		{
			name: "has color",
			annotations: &notion.Annotations{
				Color: notion.ColorBlue,
			},
			expected: true,
		},
		{
			name: "no annotations",
			annotations: &notion.Annotations{
				Color: notion.ColorDefault,
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := hasAnnotation(test.annotations)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestReverseString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello",
			expected: "olleh",
		},
		{
			name:     "markdown annotation",
			input:    "**",
			expected: "**",
		},
		{
			name:     "complex annotation",
			input:    "**~~`",
			expected: "`~~**",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single character",
			input:    "a",
			expected: "a",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := reverseString(test.input)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestExtractPlainTextFromRichText(t *testing.T) {
	tests := []struct {
		name     string
		richText []notion.RichText
		expected string
	}{
		{
			name: "single text",
			richText: []notion.RichText{
				{
					PlainText: "Hello",
				},
			},
			expected: "Hello",
		},
		{
			name: "multiple texts",
			richText: []notion.RichText{
				{
					PlainText: "Hello",
				},
				{
					PlainText: " ",
				},
				{
					PlainText: "World",
				},
			},
			expected: "Hello World",
		},
		{
			name:     "empty array",
			richText: []notion.RichText{},
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := extractPlainTextFromRichText(test.richText)
			assert.Equal(t, test.expected, result)
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}

func boolPtr(b bool) *bool {
	return &b
}

func strPtr(s string) *string {
	return &s
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestPropertiesToFrontMatter(t *testing.T) {
	tests := []struct {
		name       string
		properties notion.DatabasePageProperties
		keys       []string
		expected   string
	}{
		{
			name: "select property",
			properties: notion.DatabasePageProperties{
				"Status": notion.DatabasePageProperty{
					Type: notion.DBPropTypeSelect,
					Select: &notion.SelectOptions{
						Name: "In Progress",
					},
				},
			},
			keys:     []string{"Status"},
			expected: "---\nStatus: In Progress\n---\n",
		},
		{
			name: "multi-select property",
			properties: notion.DatabasePageProperties{
				"Tags": notion.DatabasePageProperty{
					Type: notion.DBPropTypeMultiSelect,
					MultiSelect: []notion.SelectOptions{
						{Name: "work"},
						{Name: "urgent"},
					},
				},
			},
			keys:     []string{"Tags"},
			expected: "---\nTags: [work,urgent]\n---\n",
		},
		{
			name: "status property",
			properties: notion.DatabasePageProperties{
				"Status": notion.DatabasePageProperty{
					Type: notion.DBPropTypeStatus,
					Status: &notion.SelectOptions{
						Name: "Done",
					},
				},
			},
			keys:     []string{"Status"},
			expected: "---\nStatus: Done\n---\n",
		},
		{
			name: "rich text property",
			properties: notion.DatabasePageProperties{
				"Description": notion.DatabasePageProperty{
					Type: notion.DBPropTypeRichText,
					RichText: []notion.RichText{
						{PlainText: "Test description"},
					},
				},
			},
			keys:     []string{"Description"},
			expected: "---\nDescription: Test description\n---\n",
		},
		{
			name: "url property",
			properties: notion.DatabasePageProperties{
				"Website": notion.DatabasePageProperty{
					Type: notion.DBPropTypeURL,
					URL:  strPtr("https://example.com"),
				},
			},
			keys:     []string{"Website"},
			expected: "---\nWebsite: https://example.com\n---\n",
		},
		{
			name: "email property",
			properties: notion.DatabasePageProperties{
				"Contact": notion.DatabasePageProperty{
					Type:  notion.DBPropTypeEmail,
					Email: strPtr("test@example.com"),
				},
			},
			keys:     []string{"Contact"},
			expected: "---\nContact: test@example.com\n---\n",
		},
		{
			name: "phone number property",
			properties: notion.DatabasePageProperties{
				"Phone": notion.DatabasePageProperty{
					Type:        notion.DBPropTypePhoneNumber,
					PhoneNumber: strPtr("+1234567890"),
				},
			},
			keys:     []string{"Phone"},
			expected: "---\nPhone: +1234567890\n---\n",
		},
		{
			name: "checkbox property true",
			properties: notion.DatabasePageProperties{
				"Completed": notion.DatabasePageProperty{
					Type:     notion.DBPropTypeCheckbox,
					Checkbox: boolPtr(true),
				},
			},
			keys:     []string{"Completed"},
			expected: "---\nCompleted: true\n---\n",
		},
		{
			name: "checkbox property false",
			properties: notion.DatabasePageProperties{
				"Archived": notion.DatabasePageProperty{
					Type:     notion.DBPropTypeCheckbox,
					Checkbox: boolPtr(false),
				},
			},
			keys:     []string{"Archived"},
			expected: "---\nArchived: false\n---\n",
		},
		{
			name: "number property",
			properties: notion.DatabasePageProperties{
				"Count": notion.DatabasePageProperty{
					Type:   notion.DBPropTypeNumber,
					Number: float64Ptr(42.5),
				},
			},
			keys:     []string{"Count"},
			expected: "---\nCount: 42.500000\n---\n",
		},
		{
			name: "date property without time",
			properties: notion.DatabasePageProperties{
				"DueDate": notion.DatabasePageProperty{
					Type: notion.DBPropTypeDate,
					Date: &notion.Date{
						Start: notion.DateTime{Time: mustParseTime("2006-01-02", "2024-03-15")},
					},
				},
			},
			keys:     []string{"DueDate"},
			expected: "---\nDueDate: 2024-03-15\n---\n",
		},
		{
			name: "created time property",
			properties: notion.DatabasePageProperties{
				"Created": notion.DatabasePageProperty{
					Type:        notion.DBPropTypeCreatedTime,
					CreatedTime: timePtr(mustParseTime("2006-01-02", "2024-01-01")),
				},
			},
			keys:     []string{"Created"},
			expected: "---\nCreated: 2024-01-01 00:00:00 +0000 UTC\n---\n",
		},
		{
			name: "created by property",
			properties: notion.DatabasePageProperties{
				"Author": notion.DatabasePageProperty{
					Type: notion.DBPropTypeCreatedBy,
					CreatedBy: &notion.User{
						Name: "John Doe",
					},
				},
			},
			keys:     []string{"Author"},
			expected: "---\nAuthor: John Doe\n---\n",
		},
		{
			name: "last edited time property",
			properties: notion.DatabasePageProperties{
				"Modified": notion.DatabasePageProperty{
					Type:           notion.DBPropTypeLastEditedTime,
					LastEditedTime: timePtr(mustParseTime("2006-01-02", "2024-02-15")),
				},
			},
			keys:     []string{"Modified"},
			expected: "---\nModified: 2024-02-15 00:00:00 +0000 UTC\n---\n",
		},
		{
			name: "last edited by property",
			properties: notion.DatabasePageProperties{
				"Editor": notion.DatabasePageProperty{
					Type: notion.DBPropTypeLastEditedBy,
					LastEditedBy: &notion.User{
						Name: "Jane Smith",
					},
				},
			},
			keys:     []string{"Editor"},
			expected: "---\nEditor: Jane Smith\n---\n",
		},
		{
			name: "rollup number property",
			properties: notion.DatabasePageProperties{
				"TotalCount": notion.DatabasePageProperty{
					Type: notion.DBPropTypeRollup,
					Rollup: &notion.RollupResult{
						Type:   notion.RollupResultTypeNumber,
						Number: float64Ptr(100.5),
					},
				},
			},
			keys:     []string{"TotalCount"},
			expected: "---\nTotalCount: 100.500000\n---\n",
		},
		{
			name: "rollup date property",
			properties: notion.DatabasePageProperties{
				"LatestDate": notion.DatabasePageProperty{
					Type: notion.DBPropTypeRollup,
					Rollup: &notion.RollupResult{
						Type: notion.RollupResultTypeDate,
						Date: &notion.Date{
							Start: notion.DateTime{Time: mustParseTime("2006-01-02", "2024-06-01")},
						},
					},
				},
			},
			keys:     []string{"LatestDate"},
			expected: "---\nLatestDate: 2024-06-01\n---\n",
		},
		{
			name: "rollup array property",
			properties: notion.DatabasePageProperties{
				"Numbers": notion.DatabasePageProperty{
					Type: notion.DBPropTypeRollup,
					Rollup: &notion.RollupResult{
						Type: notion.RollupResultTypeArray,
						Array: []notion.DatabasePageProperty{
							{Type: notion.DBPropTypeNumber, Number: float64Ptr(1.5)},
							{Type: notion.DBPropTypeNumber, Number: float64Ptr(2.5)},
							{Type: notion.DBPropTypeNumber, Number: float64Ptr(3.5)},
						},
					},
				},
			},
			keys:     []string{"Numbers"},
			expected: "---\nNumbers: [1.500000 2.500000 3.500000]\n---\n",
		},
		{
			name: "select property nil",
			properties: notion.DatabasePageProperties{
				"Status": notion.DatabasePageProperty{
					Type:   notion.DBPropTypeSelect,
					Select: nil,
				},
			},
			keys:     []string{"Status"},
			expected: "---\n---\n",
		},
		{
			name: "date property nil",
			properties: notion.DatabasePageProperties{
				"DueDate": notion.DatabasePageProperty{
					Type: notion.DBPropTypeDate,
					Date: nil,
				},
			},
			keys:     []string{"DueDate"},
			expected: "---\n---\n",
		},
		{
			name: "url property nil",
			properties: notion.DatabasePageProperties{
				"Website": notion.DatabasePageProperty{
					Type: notion.DBPropTypeURL,
					URL:  nil,
				},
			},
			keys:     []string{"Website"},
			expected: "---\nWebsite: \n---\n",
		},
		{
			name: "email property nil",
			properties: notion.DatabasePageProperties{
				"Contact": notion.DatabasePageProperty{
					Type:  notion.DBPropTypeEmail,
					Email: nil,
				},
			},
			keys:     []string{"Contact"},
			expected: "---\nContact: \n---\n",
		},
		{
			name: "phone property nil",
			properties: notion.DatabasePageProperties{
				"Phone": notion.DatabasePageProperty{
					Type:        notion.DBPropTypePhoneNumber,
					PhoneNumber: nil,
				},
			},
			keys:     []string{"Phone"},
			expected: "---\nPhone: \n---\n",
		},
		{
			name: "number property nil",
			properties: notion.DatabasePageProperties{
				"Count": notion.DatabasePageProperty{
					Type:   notion.DBPropTypeNumber,
					Number: nil,
				},
			},
			keys:     []string{"Count"},
			expected: "---\nCount: \n---\n",
		},
		{
			name: "checkbox property nil",
			properties: notion.DatabasePageProperties{
				"Checked": notion.DatabasePageProperty{
					Type:     notion.DBPropTypeCheckbox,
					Checkbox: nil,
				},
			},
			keys:     []string{"Checked"},
			expected: "---\nChecked: \n---\n",
		},
		{
			name: "title property",
			properties: notion.DatabasePageProperties{
				"Name": notion.DatabasePageProperty{
					Type: notion.DBPropTypeTitle,
					Title: []notion.RichText{
						{PlainText: "Page Title"},
					},
				},
			},
			keys:     []string{"Name"},
			expected: "---\nName: Page Title\n---\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			buffer := &strings.Builder{}
			m.propertiesToFrontMatter(context.Background(), parentPage, test.keys, test.properties, buffer)

			assert.Equal(t, test.expected, buffer.String())
		})
	}
}

func TestPageToMarkdown_VariousBlocks(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []notion.Block
		expected string
	}{
		{
			name: "divider block",
			blocks: []notion.Block{
				&notion.DividerBlock{},
			},
			expected: "---\n",
		},
		{
			name: "equation block",
			blocks: []notion.Block{
				&notion.EquationBlock{
					Expression: "E=mc^2",
				},
			},
			expected: "$$E=mc^2$$\n",
		},
		{
			name: "file block external",
			blocks: []notion.Block{
				&notion.FileBlock{
					Type: notion.FileTypeExternal,
					External: &notion.FileExternal{
						URL: "https://example.com/file.pdf",
					},
				},
			},
			expected: "![](https://example.com/file.pdf)\n",
		},
		{
			name: "video block external",
			blocks: []notion.Block{
				&notion.VideoBlock{
					Type: notion.FileTypeExternal,
					External: &notion.FileExternal{
						URL: "https://example.com/video.mp4",
					},
				},
			},
			expected: "![](https://example.com/video.mp4)\n",
		},
		{
			name: "embed block",
			blocks: []notion.Block{
				&notion.EmbedBlock{
					URL: "https://example.com/embed",
				},
			},
			expected: "![](https://example.com/embed)\n",
		},
		{
			name: "bookmark block",
			blocks: []notion.Block{
				&notion.BookmarkBlock{
					URL: "https://example.com",
				},
			},
			expected: "![](https://example.com)\n",
		},
		{
			name: "link preview block",
			blocks: []notion.Block{
				&notion.LinkPreviewBlock{
					URL: "https://example.com/preview",
				},
			},
			expected: "![](https://example.com/preview)\n",
		},
		{
			name: "child page block",
			blocks: []notion.Block{
				&notion.ChildPageBlock{
					Title: "Child Page Title",
				},
			},
			expected: "[[Child Page Title]]\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, false)

			require.NoError(t, err)
			assert.Equal(t, test.expected, parentPage.buffer.String())
		})
	}
}

func TestDebugLog(t *testing.T) {
	tests := []struct {
		name        string
		debugMode   bool
		message     string
		shouldLog   bool
	}{
		{
			name:        "debug mode enabled",
			debugMode:   true,
			message:     "Debug message",
			shouldLog:   true,
		},
		{
			name:        "debug mode disabled",
			debugMode:   false,
			message:     "Debug message",
			shouldLog:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, buffer := log.MockLogger()
			m := &migrator{
				config: &config.Config{
					Debug: test.debugMode,
				},
				logger: logger,
			}

			m.debugLog(test.message)

			bytes, _ := io.ReadAll(buffer)
			output := string(bytes)
			if test.shouldLog {
				assert.Contains(t, output, test.message)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestPageToMarkdown_HeadingBlocks(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []notion.Block
		expected string
	}{
		{
			name: "heading 1",
			blocks: []notion.Block{
				&notion.Heading1Block{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Heading 1",
							Text:        &notion.Text{Content: "Heading 1"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			expected: "# Heading 1\n",
		},
		{
			name: "heading 2",
			blocks: []notion.Block{
				&notion.Heading2Block{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Heading 2",
							Text:        &notion.Text{Content: "Heading 2"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			expected: "## Heading 2\n",
		},
		{
			name: "heading 3",
			blocks: []notion.Block{
				&notion.Heading3Block{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Heading 3",
							Text:        &notion.Text{Content: "Heading 3"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			expected: "### Heading 3\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, false)

			require.NoError(t, err)
			assert.Equal(t, test.expected, parentPage.buffer.String())
		})
	}
}

func TestPageToMarkdown_ListBlocks(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []notion.Block
		expected string
	}{
		{
			name: "bulleted list",
			blocks: []notion.Block{
				&notion.BulletedListItemBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "List item",
							Text:        &notion.Text{Content: "List item"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			expected: "- List item\n",
		},
		{
			name: "numbered list",
			blocks: []notion.Block{
				&notion.NumberedListItemBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Numbered item",
							Text:        &notion.Text{Content: "Numbered item"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			expected: "- Numbered item\n",
		},
		{
			name: "todo checked",
			blocks: []notion.Block{
				&notion.ToDoBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Done task",
							Text:        &notion.Text{Content: "Done task"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
					Checked: boolPtr(true),
				},
			},
			expected: "- [x] Done task\n",
		},
		{
			name: "todo unchecked",
			blocks: []notion.Block{
				&notion.ToDoBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Pending task",
							Text:        &notion.Text{Content: "Pending task"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
					Checked: boolPtr(false),
				},
			},
			expected: "- [ ] Pending task\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, false)

			require.NoError(t, err)
			assert.Equal(t, test.expected, parentPage.buffer.String())
		})
	}
}

func TestPageToMarkdown_TextBlocks(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []notion.Block
		indent   bool
		expected string
	}{
		{
			name: "paragraph",
			blocks: []notion.Block{
				&notion.ParagraphBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Paragraph text",
							Text:        &notion.Text{Content: "Paragraph text"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			indent:   false,
			expected: "Paragraph text\n",
		},
		{
			name: "paragraph with indent",
			blocks: []notion.Block{
				&notion.ParagraphBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Indented text",
							Text:        &notion.Text{Content: "Indented text"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			indent:   true,
			expected: "\tIndented text\n",
		},
		{
			name: "empty paragraph",
			blocks: []notion.Block{
				&notion.ParagraphBlock{
					RichText: []notion.RichText{},
				},
			},
			indent:   false,
			expected: "\n",
		},
		{
			name: "quote",
			blocks: []notion.Block{
				&notion.QuoteBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Quoted text",
							Text:        &notion.Text{Content: "Quoted text"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			indent:   false,
			expected: "> Quoted text\n",
		},
		{
			name: "toggle",
			blocks: []notion.Block{
				&notion.ToggleBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Toggle text",
							Text:        &notion.Text{Content: "Toggle text"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
				},
			},
			indent:   false,
			expected: "- Toggle text\n",
		},
		{
			name: "callout with emoji",
			blocks: []notion.Block{
				&notion.CalloutBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "Important",
							Text:        &notion.Text{Content: "Important"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
					Icon: &notion.Icon{
						Emoji: strPtr("⚠️"),
					},
				},
			},
			indent:   false,
			expected: "> [!⚠️Important]\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, test.indent)

			require.NoError(t, err)
			assert.Equal(t, test.expected, parentPage.buffer.String())
		})
	}
}

func TestPageToMarkdown_CodeAndMedia(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []notion.Block
		expected string
	}{
		{
			name: "code block",
			blocks: []notion.Block{
				&notion.CodeBlock{
					RichText: []notion.RichText{
						{
							Type:        notion.RichTextTypeText,
							PlainText:   "fmt.Println(\"Hello\")",
							Text:        &notion.Text{Content: "fmt.Println(\"Hello\")"},
							Annotations: &notion.Annotations{Color: notion.ColorDefault},
						},
					},
					Language: strPtr("go"),
				},
			},
			expected: "```go\nfmt.Println(\"Hello\")\n```\n",
		},
		{
			name: "image external",
			blocks: []notion.Block{
				&notion.ImageBlock{
					Type: notion.FileTypeExternal,
					External: &notion.FileExternal{
						URL: "https://example.com/image.png",
					},
				},
			},
			expected: "![](https://example.com/image.png)\n",
		},
		{
			name: "pdf external",
			blocks: []notion.Block{
				&notion.PDFBlock{
					Type: notion.FileTypeExternal,
					External: &notion.FileExternal{
						URL: "https://example.com/doc.pdf",
					},
				},
			},
			expected: "![](https://example.com/doc.pdf)\n",
		},
		{
			name: "child database",
			blocks: []notion.Block{
				&notion.ChildDatabaseBlock{
					Title: "Database Title",
				},
			},
			expected: "Database Title\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, false)

			require.NoError(t, err)
			assert.Equal(t, test.expected, parentPage.buffer.String())
		})
	}
}

func TestPageToMarkdown_StoreImages(t *testing.T) {
	logger, _ := log.MockLogger()
	tmpDir := t.TempDir()

	m := &migrator{
		config: &config.Config{
			StoreImages: true,
			VaultPath:   tmpDir,
		},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
		title:  "TestPage",
		images: []*image{},
	}

	blocks := []notion.Block{
		&notion.ImageBlock{
			Type: notion.FileTypeFile,
			File: &notion.FileFile{
				URL: "https://example.com/stored-image.png",
			},
		},
	}

	ctx := context.Background()
	err := m.pageToMarkdown(ctx, parentPage, blocks, false)

	require.NoError(t, err)
	assert.Contains(t, parentPage.buffer.String(), "![[Images/TestPage/")
	assert.Contains(t, parentPage.buffer.String(), ".png]]")
	assert.Len(t, parentPage.images, 1)
	assert.Equal(t, "https://example.com/stored-image.png", parentPage.images[0].url)
}

func TestPageToMarkdown_StorePDF(t *testing.T) {
	logger, _ := log.MockLogger()
	tmpDir := t.TempDir()

	m := &migrator{
		config: &config.Config{
			StoreImages: true,
			VaultPath:   tmpDir,
		},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
		title:  "TestPage",
		images: []*image{},
	}

	blocks := []notion.Block{
		&notion.PDFBlock{
			Type: notion.FileTypeFile,
			File: &notion.FileFile{
				URL: "https://example.com/document.pdf",
			},
		},
	}

	ctx := context.Background()
	err := m.pageToMarkdown(ctx, parentPage, blocks, false)

	require.NoError(t, err)
	assert.Contains(t, parentPage.buffer.String(), "![[Images/TestPage/")
	assert.Contains(t, parentPage.buffer.String(), ".pdf]]")
	assert.Len(t, parentPage.images, 1)
	assert.Equal(t, "https://example.com/document.pdf", parentPage.images[0].url)
}

func TestPropertiesToFrontMatter_RollupUnsupported(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	properties := notion.DatabasePageProperties{
		"Unsupported": notion.DatabasePageProperty{
			Type: notion.DBPropTypeRollup,
			Rollup: &notion.RollupResult{
				Type: notion.RollupResultTypeUnsupported,
			},
		},
		"Incomplete": notion.DatabasePageProperty{
			Type: notion.DBPropTypeRollup,
			Rollup: &notion.RollupResult{
				Type: notion.RollupResultTypeIncomplete,
			},
		},
	}

	ctx := context.Background()
	m.propertiesToFrontMatter(ctx, parentPage, []string{"Unsupported", "Incomplete"}, properties, parentPage.buffer)

	// Unsupported and Incomplete rollups should not produce output
	assert.Equal(t, "---\n---\n", parentPage.buffer.String())
}

func TestPropertiesToFrontMatter_People(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	properties := notion.DatabasePageProperties{
		"People": notion.DatabasePageProperty{
			Type: notion.DBPropTypePeople,
		},
	}

	ctx := context.Background()
	m.propertiesToFrontMatter(ctx, parentPage, []string{"People"}, properties, parentPage.buffer)

	// People property should not produce output
	assert.Equal(t, "---\n---\n", parentPage.buffer.String())
}

func TestPropertiesToFrontMatter_Files(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	properties := notion.DatabasePageProperties{
		"Files": notion.DatabasePageProperty{
			Type: notion.DBPropTypeFiles,
		},
	}

	ctx := context.Background()
	m.propertiesToFrontMatter(ctx, parentPage, []string{"Files"}, properties, parentPage.buffer)

	// Files property should not produce output
	assert.Equal(t, "---\n---\n", parentPage.buffer.String())
}

func TestPropertiesToFrontMatter_Formula(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	properties := notion.DatabasePageProperties{
		"Formula": notion.DatabasePageProperty{
			Type: notion.DBPropTypeFormula,
		},
	}

	ctx := context.Background()
	m.propertiesToFrontMatter(ctx, parentPage, []string{"Formula"}, properties, parentPage.buffer)

	// Formula property should not produce output
	assert.Equal(t, "---\n---\n", parentPage.buffer.String())
}

func TestPageToMarkdown_ColumnBlocks(t *testing.T) {
	httpClient := &http.Client{
		Transport: &mockRoundtripper{fn: func(r *http.Request) (*http.Response, error) {
			// Return empty children for column blocks
			respBody := `{"results": [], "has_more": false}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader([]byte(respBody))),
			}, nil
		}},
	}

	notionClient := notion.NewClient("test-token", notion.WithHTTPClient(httpClient))
	logger, _ := log.MockLogger()
	m := &migrator{
		config:       &config.Config{},
		logger:       logger,
		notionClient: notionClient,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	tests := []struct {
		name   string
		blocks []notion.Block
	}{
		{
			name: "column list block",
			blocks: []notion.Block{
				&notion.ColumnListBlock{},
			},
		},
		{
			name: "column block",
			blocks: []notion.Block{
				&notion.ColumnBlock{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parentPage.buffer.Reset()
			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, false)
			require.NoError(t, err)
		})
	}
}

func TestPageToMarkdown_UnsupportedBlocks(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	tests := []struct {
		name   string
		blocks []notion.Block
	}{
		{
			name: "table of contents",
			blocks: []notion.Block{
				&notion.TableOfContentsBlock{},
			},
		},
		{
			name: "breadcrumb",
			blocks: []notion.Block{
				&notion.BreadcrumbBlock{},
			},
		},
		{
			name: "unsupported",
			blocks: []notion.Block{
				&notion.UnsupportedBlock{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parentPage.buffer.Reset()
			ctx := context.Background()
			err := m.pageToMarkdown(ctx, parentPage, test.blocks, false)
			require.NoError(t, err)
			// These blocks should not produce output
			assert.Empty(t, parentPage.buffer.String())
		})
	}
}

func TestWriteRichText_MentionTypes(t *testing.T) {
	tests := []struct {
		name     string
		richText []notion.RichText
		expected string
	}{
		{
			name: "mention database",
			richText: []notion.RichText{
				{
					Type:      notion.RichTextTypeMention,
					PlainText: "My Database",
					Annotations: &notion.Annotations{
						Color: notion.ColorDefault,
					},
					Mention: &notion.Mention{
						Type: notion.MentionTypeDatabase,
					},
				},
			},
			expected: "[[My Database]]",
		},
		{
			name: "mention date",
			richText: []notion.RichText{
				{
					Type:      notion.RichTextTypeMention,
					PlainText: "2024-01-15",
					Annotations: &notion.Annotations{
						Color: notion.ColorDefault,
					},
					Mention: &notion.Mention{
						Type: notion.MentionTypeDate,
						Date: &notion.Date{
							Start: notion.DateTime{Time: mustParseTime("2006-01-02", "2024-01-15")},
						},
					},
				},
			},
			expected: "[[2024-01-15]]",
		},
		{
			name: "mention link preview",
			richText: []notion.RichText{
				{
					Type:      notion.RichTextTypeMention,
					PlainText: "Link Preview",
					Annotations: &notion.Annotations{
						Color: notion.ColorDefault,
					},
					Mention: &notion.Mention{
						Type: notion.MentionTypeLinkPreview,
						LinkPreview: &notion.LinkPreview{
							URL: "https://example.com",
						},
					},
				},
			},
			expected: "https://example.com",
		},
		{
			name: "mention template",
			richText: []notion.RichText{
				{
					Type:      notion.RichTextTypeMention,
					PlainText: "Template",
					Annotations: &notion.Annotations{
						Color: notion.ColorDefault,
					},
					Mention: &notion.Mention{
						Type: notion.MentionTypeTemplateMention,
					},
				},
			},
			expected: "",
		},
		{
			name: "mention user",
			richText: []notion.RichText{
				{
					Type:      notion.RichTextTypeMention,
					PlainText: "User",
					Annotations: &notion.Annotations{
						Color: notion.ColorDefault,
					},
					Mention: &notion.Mention{
						Type: notion.MentionTypeUser,
					},
				},
			},
			expected: "",
		},
		{
			name: "equation",
			richText: []notion.RichText{
				{
					Type:      notion.RichTextTypeEquation,
					PlainText: "E=mc^2",
					Annotations: &notion.Annotations{
						Color: notion.ColorDefault,
					},
					Equation: &notion.Equation{
						Expression: "E=mc^2",
					},
				},
			},
			expected: "$$E=mc^2$$",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := log.MockLogger()
			m := &migrator{
				config: &config.Config{},
				logger: logger,
				cache:  NewCache(),
			}

			parentPage := &Page{
				buffer: &strings.Builder{},
			}

			ctx := context.Background()
			err := m.writeRichText(ctx, parentPage, test.richText)
			require.NoError(t, err)
			assert.Equal(t, test.expected, parentPage.buffer.String())
		})
	}
}

func TestWriteRichText_TextWithLink(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
		cache:  NewCache(),
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	richText := []notion.RichText{
		{
			Type:      notion.RichTextTypeText,
			PlainText: "Click here",
			Annotations: &notion.Annotations{
				Color: notion.ColorDefault,
			},
			Text: &notion.Text{
				Content: "Click here",
				Link: &notion.Link{
					URL: "https://example.com",
				},
			},
		},
	}

	ctx := context.Background()
	err := m.writeRichText(ctx, parentPage, richText)
	require.NoError(t, err)
	assert.Equal(t, "[Click here](https://example.com)", parentPage.buffer.String())
}

func TestWriteRichText_CodeWithLink(t *testing.T) {
	logger, _ := log.MockLogger()
	m := &migrator{
		config: &config.Config{},
		logger: logger,
		cache:  NewCache(),
	}

	parentPage := &Page{
		buffer: &strings.Builder{},
	}

	richText := []notion.RichText{
		{
			Type:      notion.RichTextTypeText,
			PlainText: "code",
			Annotations: &notion.Annotations{
				Code:  true,
				Color: notion.ColorDefault,
			},
			Text: &notion.Text{
				Content: "code",
				Link: &notion.Link{
					URL: "https://example.com",
				},
			},
		},
	}

	ctx := context.Background()
	err := m.writeRichText(ctx, parentPage, richText)
	require.NoError(t, err)
	// When code annotation is present, the link should be ignored
	assert.Equal(t, "`code`", parentPage.buffer.String())
}

func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}
