package migrator

import (
	"bufio"
	"bytes"
	"context"
	"testing"

	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/dstotijn/go-notion"
)

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
