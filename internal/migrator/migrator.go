package migrator

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GustavoCaso/n2o/internal/config"
	"github.com/GustavoCaso/n2o/internal/log"
	"github.com/dstotijn/go-notion"
	"github.com/itchyny/timefmt-go"
)

type image struct {
	external bool
	url      string
	name     string
}

type Page struct {
	Path       string
	title      string
	id         string
	buffer     *strings.Builder
	notionPage notion.Page
	coverPhoto *image
	images     []*image
	parent     *Page
	children   []*Page
}

func (p *Page) String() string {
	childPages := make([]string, len(p.children))
	for i, page := range p.children {
		childPages[i] = page.String()
	}

	return fmt.Sprintf("%s child pages: %s", p.Path, childPages)
}

type Migrator interface {
	FetchPages(ctx context.Context) ([]*Page, error)
	FetchParseAndSavePage(ctx context.Context, page *Page, pageProperties map[string]bool) error
	DisplayInformation(ctx context.Context) error
	WritePagesToDisk(ctx context.Context) error
}

type migrator struct {
	notionClient *notion.Client
	config       *config.Config
	cache        *Cache
	pages        []*Page
	logger       log.Log
	httpClient   *http.Client
}

func NewMigrator(config *config.Config, cache *Cache, logger log.Log) Migrator {
	notionClient := notion.NewClient(config.Token)

	return &migrator{
		notionClient: notionClient,
		config:       config,
		cache:        cache,
		logger:       logger,
		httpClient:   http.DefaultClient,
	}
}

func (m *migrator) FetchPages(ctx context.Context) ([]*Page, error) {
	if m.config.DatabaseID != "" {
		db, err := m.notionClient.FindDatabaseByID(ctx, m.config.DatabaseID)
		if err != nil {
			return []*Page{}, fmt.Errorf("failed to get DB %s. error: %s", m.config.DatabaseID, err.Error())
		}
		dbTitle := extractPlainTextFromRichText(db.Title)
		notionPages, err := m.fetchNotionDBPages(ctx)
		if err != nil {
			return []*Page{}, fmt.Errorf(
				"failed to get pages from DB %s. error: %s",
				m.config.DatabaseID,
				err.Error(),
			)
		}
		pages := make([]*Page, len(notionPages))

		for i, notionPage := range notionPages {
			title := m.extractPageTitle(notionPage)
			page := &Page{
				id:         notionPage.ID,
				buffer:     &strings.Builder{},
				title:      title,
				Path:       path.Join(m.config.VaultFilepath(), dbTitle, title),
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

		m.pages = pages

		return pages, nil
	}
	notionPage, err := m.notionClient.FindPageByID(context.Background(), m.config.PageID)
	if err != nil {
		return []*Page{}, fmt.Errorf(
			"failed to find the page %s make sure the page exists in your Notioin workspace. error: %s",
			m.config.PageID,
			err.Error(),
		)
	}

	var cover *image
	if notionPage.Cover != nil {
		cover = &image{
			external: true,
			url:      notionPage.Cover.External.URL,
		}
	}

	title := m.extractPageTitle(notionPage)
	pages := []*Page{
		{
			id:         notionPage.ID,
			buffer:     &strings.Builder{},
			title:      title,
			Path:       path.Join(m.config.VaultFilepath(), title),
			notionPage: notionPage,
			parent:     nil,
			coverPhoto: cover,
		},
	}
	m.pages = pages

	return pages, nil
}

func (m *migrator) extractPageTitle(page notion.Page) string {
	var str string

	switch page.Parent.Type {
	case notion.ParentTypeDatabase:
		properties, ok := page.Properties.(notion.DatabasePageProperties)
		if !ok {
			m.logger.Error(fmt.Sprintf("expected DatabasePageProperties, got %T", page.Properties))
			return "untitled.md"
		}
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

			val, ok := m.config.PageNameFilters[strings.ToLower(key)]

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
					m.logger.Info(fmt.Sprintf("type: `%s` for extracting page title not supported\n", value.Type))
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
		properties, ok := page.Properties.(notion.PageProperties)
		if !ok {
			m.logger.Error(fmt.Sprintf("expected PageProperties, got %T", page.Properties))
			return "untitled.md"
		}
		str = extractPlainTextFromRichText(properties.Title.Title)
	}

	fileName := fmt.Sprintf("%s.md", str)
	return fileName
}

func (m *migrator) FetchParseAndSavePage(ctx context.Context, page *Page, pageProperties map[string]bool) error {
	pageBlocks, err := m.notionClient.FindBlockChildrenByID(ctx, page.notionPage.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to extract children blocks for block ID %s. error: %w", page.notionPage.ID, err)
	}

	if page.notionPage.Parent.Type == notion.ParentTypeDatabase && len(pageProperties) > 0 {
		props, ok := page.notionPage.Properties.(notion.DatabasePageProperties)
		if !ok {
			return fmt.Errorf("expected DatabasePageProperties, got %T", page.notionPage.Properties)
		}

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
		fmt.Fprintf(page.buffer, "![700x200](%s)", page.coverPhoto.url)
		page.buffer.WriteString("\n\n")
	}

	err = m.pageToMarkdown(ctx, page, pageBlocks.Results, false)

	if err != nil {
		return fmt.Errorf("failed to convert page to markdown. error: %w", err)
	}

	return nil
}

func (m *migrator) DisplayInformation(_ context.Context) error {
	buffer := bufio.NewWriter(os.Stdout)
	for _, page := range m.pages {
		if _, err := fmt.Fprintf(buffer, "%s \n", m.removeObsidianVault(page.Path)); err != nil {
			return fmt.Errorf("failed to write page path: %w", err)
		}
		m.displayPageInfo(page, buffer, 0)
	}

	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("failed to write into stdout. error: %w", err)
	}

	return nil
}

const spaceCount = 4
const pageIndex = 2

func (m *migrator) displayPageInfo(page *Page, buffer *bufio.Writer, index int) {
	var spaces int
	if index > 0 {
		spaces = index * spaceCount
	} else {
		spaces = spaceCount
	}
	for _, childPage := range page.children {
		if _, err := fmt.Fprintf(buffer, "%*s %s\n", spaces, "|->", m.removeObsidianVault(childPage.Path)); err != nil {
			m.logger.Error(fmt.Sprintf("failed to write child page path: %v", err))
		}
		m.displayPageInfo(childPage, buffer, pageIndex)
	}
}

func (m *migrator) removeObsidianVault(s string) string {
	return strings.TrimPrefix(s, m.config.VaultPath+"/")
}

func (m *migrator) WritePagesToDisk(_ context.Context) error {
	for _, page := range m.pages {
		err := m.writePage(page)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *migrator) writePage(page *Page) error {
	if err := os.MkdirAll(filepath.Dir(page.Path), 0750); err != nil {
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

	if m.config.StoreImages {
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

const Untitled = "Untitled"

// fetchPage check if the page has been extracted before avoiding doing to many queries to notion.so
// If the page is not stored in cached will download the page.
// If the page title has been provided it will use it, if is an empty string it will try to extracted from the page information.
// The logic to extract the title varies depending on the page parent's type.
// TODO: handle better the quote logic
// handlePageParent processes the parent type of a page and updates the childTitle accordingly.
func (m *migrator) handlePageParent(
	ctx context.Context,
	mentionPage notion.Page,
	childTitle string,
	extractTitle bool,
) (string, error) {
	switch mentionPage.Parent.Type {
	case notion.ParentTypeDatabase:
		if extractTitle {
			childTitle = m.extractPageTitle(mentionPage)
		}
		// Since we are migrating from the same DB we do not need to create a subfolder
		// within the Obsidian vault. So we can skip fetching the database to gather
		// the name to create the subfolder
		if m.config.DatabaseID == "" || m.config.DatabaseID != mentionPage.Parent.DatabaseID {
			dbPage, err := m.notionClient.FindDatabaseByID(ctx, mentionPage.Parent.DatabaseID)
			if err != nil {
				return "", fmt.Errorf("failed to find parent db %s: %w", mentionPage.Parent.DatabaseID, err)
			}
			dbTitle := extractPlainTextFromRichText(dbPage.Title)
			childTitle = path.Join(dbTitle, childTitle)
		}
	case notion.ParentTypeBlock:
		notionParentPage, err := m.notionClient.FindPageByID(ctx, mentionPage.Parent.BlockID)
		if err != nil {
			return "", fmt.Errorf("failed to find parent block %s: %w", mentionPage.Parent.BlockID, err)
		}
		if extractTitle {
			childTitle = m.extractPageTitle(notionParentPage)
		}
	case notion.ParentTypePage:
		notionParentPage, err := m.notionClient.FindPageByID(ctx, mentionPage.Parent.PageID)
		if err != nil {
			return "", fmt.Errorf("failed to find parent mention page %s: %w", mentionPage.Parent.PageID, err)
		}
		if extractTitle {
			childTitle = m.extractPageTitle(notionParentPage)
		}
	case notion.ParentTypeWorkspace:
		if extractTitle {
			childTitle = m.extractPageTitle(mentionPage)
		}
	default:
		return "", fmt.Errorf("unsupported mention page type %s", mentionPage.Parent.Type)
	}
	return childTitle, nil
}

func (m *migrator) fetchPage(
	ctx context.Context,
	parentPage *Page,
	pageID, title string,
	buffer *strings.Builder,
	quotes bool,
) error {
	cached, ok := m.cache.Get(pageID)
	if ok {
		debugLog := fmt.Sprintf("cached page found %s\n", cached)
		if cached.parent != parentPage {
			debugLog += fmt.Sprintf("adding to parent %s\n", parentPage)
			parentPage.children = append(parentPage.children, cached)
		}
		m.debugLog(debugLog)

		var result string
		if quotes {
			result = fmt.Sprintf("\"[[%s]]\"", cached.title)
		} else {
			result = fmt.Sprintf("[[%s]]", cached.title)
		}
		buffer.WriteString(result)
		return nil
	}

	if title != "" && title == Untitled {
		// Notion pages with Untitled would return a 404 when fetching them
		// We do not process those
		m.cache.Set(pageID, &Page{
			title: Untitled,
		})
		return nil
	}

	// There could be pages that self reference them
	// We need a way to mark that a page is being work on
	// to avoid endless loop. In this case we just want to get the page title
	// TODO: Check if we need this logic
	if m.cache.IsWorking(pageID) {
		childTitle := title
		extractTitle := false
		if childTitle == "" {
			extractTitle = true
		}
		mentionPage, err := m.notionClient.FindPageByID(ctx, pageID)
		if err != nil {
			return fmt.Errorf("failed to find page %s: %w", pageID, err)
		}

		childTitle, err = m.handlePageParent(ctx, mentionPage, childTitle, extractTitle)
		if err != nil {
			return err
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

	m.cache.Mark(pageID)
	var newPage *Page

	defer func() {
		if newPage != nil {
			m.debugLog(fmt.Sprintf("saving page to cache %s\n", newPage))
			m.cache.Set(pageID, newPage)
		} else {
			m.debugLog(fmt.Sprintf("failed to save page to cache %s\n", pageID))
			m.cache.Set(pageID, &Page{
				title: Untitled,
			})
		}
	}()

	extractTitle := false
	childTitle := title
	if childTitle == "" {
		extractTitle = true
	} else if !strings.HasSuffix(childTitle, ".md") {
		childTitle = fmt.Sprintf("%s.md", childTitle)
	}

	mentionPage, err := m.notionClient.FindPageByID(ctx, pageID)
	if err != nil {
		return fmt.Errorf("failed to find page %s: %w", pageID, err)
	}

	childTitle, err = m.handlePageParent(ctx, mentionPage, childTitle, extractTitle)
	if err != nil {
		return err
	}

	if childTitle == "" {
		return fmt.Errorf("unable to find page information %s", pageID)
	}

	newPage = &Page{
		id:         pageID,
		notionPage: mentionPage,
		buffer:     &strings.Builder{},
		parent:     parentPage,
		title:      childTitle,
		Path:       path.Join(m.config.VaultFilepath(), childTitle),
	}

	if mentionPage.Cover != nil {
		newPage.coverPhoto = &image{
			external: true,
			url:      mentionPage.Cover.External.URL,
		}
	}

	parentPage.children = append(parentPage.children, newPage)

	if err = m.FetchParseAndSavePage(ctx, newPage, m.config.PagePropertiesToMigrate); err != nil {
		m.logger.Info(fmt.Sprintf("failed to fetch mention page content with page parent: %s\n", childTitle))
		return err
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

func (m *migrator) fetchNotionDBPages(ctx context.Context) ([]notion.Page, error) {
	notionResponse, err := m.notionClient.QueryDatabase(ctx, m.config.DatabaseID, nil)
	if err != nil {
		return []notion.Page{}, err
	}

	result := []notion.Page{}

	result = append(result, notionResponse.Results...)

	query := &notion.DatabaseQuery{}
	for notionResponse.HasMore {
		query.StartCursor = *notionResponse.NextCursor

		notionResponse, err = m.notionClient.QueryDatabase(ctx, m.config.DatabaseID, query)
		if err != nil {
			return []notion.Page{}, err
		}

		result = append(result, notionResponse.Results...)
	}

	return result, nil
}

func (m *migrator) downloadImage(name, url string) error {
	imageLocation := filepath.Join(m.config.VaultImagePath(), name)
	if err := os.MkdirAll(path.Dir(imageLocation), 0750); err != nil {
		return fmt.Errorf("failed to create the necessary directories to store images.  error: %w", err)
	}

	response, err := m.httpClient.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

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

func (m *migrator) debugLog(str string) {
	if m.config.Debug {
		m.logger.Debug(str)
	}
}

func extractPlainTextFromRichText(richText []notion.RichText) string {
	buffer := new(strings.Builder)

	for _, text := range richText {
		buffer.WriteString(text.PlainText)
	}

	return buffer.String()
}
