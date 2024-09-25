package migrator

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

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
				f, err := fixtures.ReadFile("fixtures/database_query.json")
				if err != nil {
					panic(err)
				}
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
				f, err := fixtures.ReadFile("fixtures/page_query.json")
				if err != nil {
					panic(err)
				}
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

func TestFetchParseAndSavePage(t *testing.T) {
	tests := []struct {
		name       string
		page       notion.Page
		statusCode int
		respBody   func(*http.Request) io.Reader
		expected   string
		config     config.Config
	}{
		{
			name:       "store page in the correct path and format markdown correctly",
			page:       notion.Page{ID: "1"},
			statusCode: 200,
			respBody: func(_ *http.Request) io.Reader {
				f, err := fixtures.ReadFile("fixtures/blocks.json")
				if err != nil {
					panic(err)
				}
				return bytes.NewReader(f)
			},
			expected: `## Lacinato kale
[Lacinato kale is a variety of kale with a long tradition in Italian cuisine, especially that of Tuscany. It is also known as Tuscan kale, Italian kale, dinosaur kale, kale, flat back kale, palm tree kale, or black Tuscan palm.](https://en.wikipedia.org/wiki/Lacinato_kale)
`,
			config: config.Config{},
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
				Cache:  cache.NewCache(),
			}

			tempDir := t.TempDir()

			destination := filepath.Join(tempDir, "example.md")

			err := migrator.FetchParseAndSavePage(context.TODO(), test.page, map[string]bool{}, destination)
			assert.NoError(t, err)

			content, err := os.ReadFile(destination)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, string(content))
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
