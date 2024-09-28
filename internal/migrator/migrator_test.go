package migrator

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GustavoCaso/n2o/internal/cache"
	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/dstotijn/go-notion"
	"github.com/stretchr/testify/assert"
)

type mockRoundtripper struct {
	fn func(*http.Request) (*http.Response, error)
}

func (m *mockRoundtripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return m.fn(r)
}

//go:embed fixtures/*
var fixtures embed.FS

func TestFetchPages(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		respBody       func(r *http.Request) io.Reader
		respStatusCode int
		config         config.Config
		hasError       bool
	}{
		{
			name: "with database ID",
			config: config.Config{
				DatabaseID: "000000",
			},
			statusCode: 200,
			respBody: func(_ *http.Request) io.Reader {
				f := mustReadFixture("fixtures/database_query.json")
				return bytes.NewReader(f)
			},
		},
		{
			name: "with database ID and error",
			config: config.Config{
				DatabaseID: "000000",
			},
			statusCode: 400,
			respBody: func(_ *http.Request) io.Reader {
				return bytes.NewBuffer([]byte{})
			},
			hasError: true,
		},
		{
			name: "with page ID",
			config: config.Config{
				PageID: "000000",
			},
			statusCode: 200,
			respBody: func(_ *http.Request) io.Reader {
				f := mustReadFixture("fixtures/page_query.json")
				return bytes.NewReader(f)
			},
		},
		{
			name: "with page ID and error",
			config: config.Config{
				PageID: "000000",
			},
			statusCode: 400,
			respBody: func(_ *http.Request) io.Reader {
				return bytes.NewBuffer([]byte{})
			},
			hasError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			httpClient := &http.Client{
				Transport: &mockRoundtripper{fn: func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: test.statusCode,
						Status:     http.StatusText(test.statusCode),
						Body:       io.NopCloser(test.respBody(r)),
					}, nil
				}},
			}

			client := notion.NewClient("secret-api-key", notion.WithHTTPClient(httpClient))

			migrator := Migrator{
				Client: client,
				Config: test.config,
				Cache:  nil,
			}

			pages, err := migrator.FetchPages(context.TODO())
			if test.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, len(pages))
				assert.IsType(t, notion.Page{}, pages[0])
			}
		})
	}
}

func TestExtractPageTitle(t *testing.T) {
	tests := []struct {
		name     string
		page     notion.Page
		expected string
		config   config.Config
	}{
		{
			name: "with database and text title",
			config: config.Config{
				DatabaseID: "0000",
				PageNameFilters: map[string]string{
					"title": "",
				},
				VaultPath:        "scr/vault",
				VaultDestination: "notes",
			},
			page: notion.Page{
				Properties: notion.DatabasePageProperties{
					"Title": notion.DatabasePageProperty{
						Type: notion.DBPropTypeTitle,
						Title: []notion.RichText{
							{
								PlainText: "Hello",
							},
						},
					},
				},
			},
			expected: "scr/vault/notes/Hello.md",
		},
		{
			name: "with database and date title",
			config: config.Config{
				DatabaseID: "0000",
				PageNameFilters: map[string]string{
					"date": "%Y/%m/%d %H:%M:%S",
				},
			},
			page: notion.Page{
				Properties: notion.DatabasePageProperties{
					"Date": notion.DatabasePageProperty{
						Type: notion.DBPropTypeDate,
						Name: "Date",
						Date: &notion.Date{
							Start: parseDateTime("2021-05-18T12:49:00.000-05:00"),
						},
					},
				},
			},
			expected: "2021/05/18 12:49:00.md",
		},
		{
			name: "with page",
			config: config.Config{
				PageID: "0000",
			},
			page: notion.Page{
				Properties: notion.PageProperties{
					Title: notion.PageTitle{
						Title: []notion.RichText{
							{
								PlainText: "Hello",
							},
						},
					},
				},
			},
			expected: "Hello.md",
		},
	}

	for _, test := range tests {
		migrator := Migrator{
			Client: nil,
			Config: test.config,
			Cache:  nil,
		}

		value := migrator.ExtractPageTitle(test.page)
		assert.Equal(t, test.expected, value)
	}
}

// TODO: Fix Realation test
// Select type
// Multi Select
func TestFetchParseAndSavePage(t *testing.T) {
	tests := []struct {
		name             string
		page             notion.Page
		statusCode       int
		respBody         func(*http.Request) io.Reader
		pageProperties   map[string]bool
		expected         string
		config           config.Config
		customAssertions func(t *testing.T, path string)
	}{
		{
			name:       "store page in the correct path and format markdown correctly",
			page:       notion.Page{ID: "1"},
			statusCode: 200,
			respBody: func(_ *http.Request) io.Reader {
				f, err := fixtures.ReadFile("fixtures/page_blocks.json")
				if err != nil {
					panic(err)
				}
				return bytes.NewReader(f)
			},
			pageProperties: map[string]bool{},
			expected: `## Lacinato kale
[Lacinato kale is a variety of kale with a long tradition in Italian cuisine, especially that of Tuscany. It is also known as Tuscan kale, Italian kale, dinosaur kale, kale, flat back kale, palm tree kale, or black Tuscan palm.](https://en.wikipedia.org/wiki/Lacinato_kale)
`,
			config: config.Config{},
		},
		{
			name: "store page with frontmatter",
			page: notion.Page{
				ID: "1",
				Parent: notion.Parent{
					Type: notion.ParentTypeDatabase,
				},
				Properties: notion.DatabasePageProperties{
					"Title": notion.DatabasePageProperty{
						Type: notion.DBPropTypeTitle,
						Title: []notion.RichText{
							{
								PlainText: "Hello",
							},
						},
					},
					"Age": notion.DatabasePageProperty{
						Type:   notion.DBPropTypeNumber,
						Number: notion.Float64Ptr(34),
					},
					"People": notion.DatabasePageProperty{
						Type: notion.DBPropTypePeople,
						Name: "People",
						People: []notion.User{
							{
								BaseUser: notion.BaseUser{
									ID: "be32e790-8292-46df-a248-b784fdf483cf",
								},
								Name:      "Jane Doe",
								AvatarURL: "https://example.com/image.png",
								Type:      notion.UserTypePerson,
								Person: &notion.Person{
									Email: "jane@example.com",
								},
							},
						},
					},
					"Files": notion.DatabasePageProperty{
						Type: notion.DBPropTypeFiles,
						Name: "Files",
						Files: []notion.File{
							{
								Name: "foobar.pdf",
							},
						},
					},
					"Checkbox": notion.DatabasePageProperty{
						ID:       "49S@",
						Type:     notion.DBPropTypeCheckbox,
						Name:     "Checkbox",
						Checkbox: notion.BoolPtr(true),
					},
					"Calculation": notion.DatabasePageProperty{
						Type: notion.DBPropTypeFormula,
						Name: "Calculation",
						Formula: &notion.FormulaResult{
							Type:   notion.FormulaResultTypeNumber,
							Number: notion.Float64Ptr(float64(42)),
						},
					},
					"URL": notion.DatabasePageProperty{
						Type: notion.DBPropTypeURL,
						Name: "URL",
						URL:  notion.StringPtr("https://example.com"),
					},
					"Email": notion.DatabasePageProperty{
						Type:  notion.DBPropTypeEmail,
						Name:  "Email",
						Email: notion.StringPtr("jane@example.com"),
					},
					"PhoneNumber": notion.DatabasePageProperty{
						Type:        notion.DBPropTypePhoneNumber,
						Name:        "PhoneNumber",
						PhoneNumber: notion.StringPtr("867-5309"),
					},
					"CreatedTime": notion.DatabasePageProperty{
						Type:        notion.DBPropTypeCreatedTime,
						Name:        "Created time",
						CreatedTime: notion.TimePtr(parseTime(time.RFC3339Nano, "2021-05-24T15:44:09.123Z")),
					},
					"CreatedBy": notion.DatabasePageProperty{
						Type: notion.DBPropTypeCreatedBy,
						Name: "Created by",
						CreatedBy: &notion.User{
							BaseUser: notion.BaseUser{
								ID: "be32e790-8292-46df-a248-b784fdf483cf",
							},
							Name:      "Jane Doe",
							AvatarURL: "https://example.com/image.png",
							Type:      notion.UserTypePerson,
							Person: &notion.Person{
								Email: "jane@example.com",
							},
						},
					},
					"LastEditedTime": notion.DatabasePageProperty{
						Type:           notion.DBPropTypeLastEditedTime,
						Name:           "Last edited time",
						LastEditedTime: notion.TimePtr(parseTime(time.RFC3339Nano, "2021-05-24T15:44:09.123Z")),
					},
					"LastEditedBy": notion.DatabasePageProperty{
						Type: notion.DBPropTypeLastEditedBy,
						Name: "Last edited by",
						LastEditedBy: &notion.User{
							BaseUser: notion.BaseUser{
								ID: "be32e790-8292-46df-a248-b784fdf483cf",
							},
							Name:      "Jane Doe",
							AvatarURL: "https://example.com/image.png",
							Type:      notion.UserTypePerson,
							Person: &notion.Person{
								Email: "jane@example.com",
							},
						},
					},
					"Relation": notion.DatabasePageProperty{
						Type: notion.DBPropTypeRelation,
						Name: "Relation",
						Relation: []notion.Relation{
							{
								ID: "2be9597f-693f-4b87-baf9-efc545d38ebe",
							},
						},
					},
					"Rollup": notion.DatabasePageProperty{
						Type: notion.DBPropTypeRollup,
						Name: "Rollup",
						Rollup: &notion.RollupResult{
							Type: notion.RollupResultTypeArray,
							Array: []notion.DatabasePageProperty{
								{
									Type:   notion.DBPropTypeNumber,
									Number: notion.Float64Ptr(42),
								},
								{
									Type:   notion.DBPropTypeNumber,
									Number: notion.Float64Ptr(10),
								},
							},
						},
					},
				},
			},
			statusCode: 200,
			respBody: func(_ *http.Request) io.Reader {
				f, err := fixtures.ReadFile("fixtures/page_blocks.json")
				if err != nil {
					panic(err)
				}
				return bytes.NewReader(f)
			},
			pageProperties: map[string]bool{
				"title":          true,
				"age":            true,
				"checkbox":       true,
				"calculation":    true,
				"url":            true,
				"email":          true,
				"phonenumber":    true,
				"createdby":      true,
				"createdtime":    true,
				"lasteditedtime": true,
				"lasteditedby":   true,
				"relation":       true,
				"rollup":         true,
			},
			expected: `---
Age: 34.000000
Checkbox: true
CreatedBy: Jane Doe
CreatedTime: 2021-05-24 15:44:09.123 +0000 UTC
Email: jane@example.com
LastEditedBy: Jane Doe
LastEditedTime: 2021-05-24 15:44:09.123 +0000 UTC
PhoneNumber: 867-5309
Relation: 
  - 
Rollup: [42.000000 10.000000]
Title: Hello
URL: https://example.com
---
## Lacinato kale
[Lacinato kale is a variety of kale with a long tradition in Italian cuisine, especially that of Tuscany. It is also known as Tuscan kale, Italian kale, dinosaur kale, kale, flat back kale, palm tree kale, or black Tuscan palm.](https://en.wikipedia.org/wiki/Lacinato_kale)
`,
			config: config.Config{},
		},
		{
			name: "store nested pages and creates subfolder in correct location",
			page: notion.Page{
				ID: "1",
			},
			statusCode: 200,
			respBody: func(r *http.Request) io.Reader {
				readFixture := func(path string) io.Reader {
					f := mustReadFixture(path)
					return bytes.NewReader(f)
				}

				switch r.URL.String() {
				case "https://api.notion.com/v1/blocks/1/children":
					// The nested page that we will fetch is called `ANSI Codes for the terminal`
					return readFixture("fixtures/page_blocks_nested_pages.json")
				case "https://api.notion.com/v1/pages/a8401073-0e1a-481f-bc9b-8093c7edadca":
					return readFixture("fixtures/nested_page.json")
				case "https://api.notion.com/v1/databases/50780e7e-09d3-4ca6-9045-86263009c971":
					// The title of the DB is Personal Notes
					// It will create a new folder and file on that location
					return readFixture("fixtures/get_database.json")
				case "https://api.notion.com/v1/blocks/17dc62b4-0331-4842-b886-af07bd576af2/children":
					return readFixture("fixtures/page_blocks.json")
				default:
					panic(fmt.Sprintf("unhandled URL: %s", r.URL.String()))
				}
			},
			customAssertions: func(t *testing.T, path string) {
				nestedPage := filepath.Join(path, "Personal Notes", "ANSI Codes for the terminal.md")
				content, err := os.ReadFile(nestedPage)
				assert.NoError(t, err)
				expectedNestedContent := `## Lacinato kale
[Lacinato kale is a variety of kale with a long tradition in Italian cuisine, especially that of Tuscany. It is also known as Tuscan kale, Italian kale, dinosaur kale, kale, flat back kale, palm tree kale, or black Tuscan palm.](https://en.wikipedia.org/wiki/Lacinato_kale)
`
				assert.Equal(t, expectedNestedContent, string(content))
			},
			pageProperties: map[string]bool{},
			expected:       string(mustReadFixture("fixtures/expected_nested_page")),
			config:         config.Config{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			httpClient := &http.Client{
				Transport: &mockRoundtripper{fn: func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: test.statusCode,
						Status:     http.StatusText(test.statusCode),
						Body:       io.NopCloser(test.respBody(r)),
					}, nil
				}},
			}

			client := notion.NewClient("secret-api-key", notion.WithHTTPClient(httpClient))

			tempDir := t.TempDir()

			test.config.VaultPath = tempDir

			migrator := Migrator{
				Client: client,
				Config: test.config,
				Cache:  cache.NewCache(),
			}

			destination := filepath.Join(tempDir, "example.md")

			err := migrator.FetchParseAndSavePage(context.TODO(), test.page, test.pageProperties, destination)
			assert.NoError(t, err)

			content, err := os.ReadFile(destination)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, string(content))
			if test.customAssertions != nil {
				test.customAssertions(t, tempDir)
			}
		})

	}
}

func TestWriteRichText_Annotations(t *testing.T) {
	migrator := Migrator{
		Client: nil,
		Config: config.Config{},
		Cache:  nil,
	}
	ctx := context.Background()

	b := &bytes.Buffer{}
	buffer := bufio.NewWriter(b)

	tests := []struct {
		name           string
		notionRichText []notion.RichText
	}{
		{
			"***[hello world](foobar)***",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Bold:   true,
						Italic: true,
						Color:  notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello world",
						Link: &notion.Link{
							URL: "foobar",
						},
					},
				},
			},
		},
		{
			"`hello world`",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Code:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello world",
						Link: &notion.Link{
							URL: "foobar",
						},
					},
				},
			},
		},
		{
			"***hello `world`***",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Bold:   true,
						Italic: true,
						Color:  notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Bold:   true,
						Italic: true,
						Code:   true,
						Color:  notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "world",
					},
				},
			},
		},
		{
			"`hello world`",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Code:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Code:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "world",
					},
				},
			},
		},
		{
			"`hello world foo`",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Code:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Code:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "world ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Code:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "foo",
					},
				},
			},
		},
		{
			"**hello **==world ==~~foo~~",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Bold:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Color: notion.ColorBlue,
					},
					Text: &notion.Text{
						Content: "world ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Color:         notion.ColorDefault,
						Strikethrough: true,
					},
					Text: &notion.Text{
						Content: "foo",
					},
				},
			},
		},
		{
			"**hello _world_**",
			[]notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Bold:  true,
						Color: notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "hello ",
					},
				},
				{
					Type: notion.RichTextTypeText,
					Annotations: &notion.Annotations{
						Italic: true,
						Bold:   true,
						Color:  notion.ColorDefault,
					},
					Text: &notion.Text{
						Content: "world",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(*testing.T) {
			err := migrator.writeRichText(ctx, buffer, test.notionRichText)

			if err != nil {
				t.Error("expected nil")
			}

			err = buffer.Flush()
			if err != nil {
				t.Error("expected nil")
			}

			result := b.String()

			if result != test.name {
				t.Errorf("incorrect result expected '%s' got: %s", test.name, result)
			}

			// Reset
			b = &bytes.Buffer{}
			buffer = bufio.NewWriter(b)
		})
	}
}

func parseDateTime(value string) notion.DateTime {
	dt, err := notion.ParseDateTime(value)
	if err != nil {
		panic(err)
	}
	return dt
}

func parseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}

func mustReadFixture(path string) []byte {
	f, err := fixtures.ReadFile(path)
	if err != nil {
		panic(err)
	}

	return f
}
