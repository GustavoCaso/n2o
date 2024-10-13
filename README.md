# Notion To Obsidian migrator (n2o)

Migration tool for your Notion workspace to Obsidian.

The current importer tool landscape usually requires you to import your entire workspace, does not allow you to select which page properties you would like to import, or fails to create a link between pages. 

With `n2o`, you have complete control over what you import from your Notion workspace to your Obsidan vault.


## Install

Download the `n2o` artifact from the [releases page](https://github.com/GustavoCaso/n2o/releases).

## Usage

```
$ n2o
Usage of n2o:
  -download-images
    	download external images to the Obsidian vault
  -notion-db-ID string
    	Notion database to migrate
  -notion-page-ID string
    	Notion page to migrate
  -notion-token string
    	Notion token
  -page-name string
    	Notion page properties to extract the Obsidian page title.
    	Support selecting different page attributes and formatting. To select multiple properties, use a comma-separated list.
    	The attributes that support custom formatting are Notion date attributes.
    	Example of how to use a Notion date property with custom format as the title for the Obsidian page:
    	-name=date:%Y/%B/%d-%A

    	By default if you do not configure any we get the Title property

  -page-properties string
    	Notion page properties to convert to Obsidian frontmater.
    	You can select multiple properties using a comma-separated list.

  -save-to-disk
    	write the pages in the Obsidian vault
  -vault-folder string
    	folder to store pages inside the Obsidian Vault
  -vault-path string
    	Obsidian vault location
```

## How do you get your Notion token?

To get your Notion token, you either have to create an integration or use an existing integration that has access to the workspace you want to migrate. The Notion website has many resources.

- [What is an Integration](https://developers.notion.com/docs/getting-started#what-is-a-notion-integration)
- [Build your first integration Guide](https://developers.notion.com/docs/create-a-notion-integration)

Once your integration is installed in your workspace, we can migrate single pages or complete databases to our Obsidian vault.

## How do I get the database ID?

Copied from the Notion website: [Retrieve a database](https://developers.notion.com/reference/retrieve-a-database)

> To find a database ID, navigate to the database URL in your Notion workspace. The ID is the string of characters in the URL that is between the slash following the workspace name (if applicable) and the question mark. The ID is a 32 characters alphanumeric string.

![databaseID](img/databaseID.png)

## How do I retrieve the Page ID?

Copied from the Notion website: [Working with page content](https://developers.notion.com/docs/working-with-page-content)
> Here's a quick procedure to find the page ID for a specific page in Notion: 
Open the page in Notion. Use the Share menu to Copy link. Now paste the link in your text editor so you can take a closer look. The URL ends in a page ID.
It should be a 32 character long string. Format this value by inserting hyphens (-) in the following pattern: 8-4-4-4-12 (each number is the length of characters between the hyphens).
Example: 1429989fe8ac4effbc8f57f56486db54 becomes 1429989f-e8ac-4eff-bc8f-57f56486db54.
This value is your page ID.

## Examples

### Get information about the pages that would be created in your Obsidian Vault
`n2o -notion-token="NOTION_TOKEN" -notion-page-ID="1429989f-e8ac-4eff-bc8f-57f56486db54" -page-properties="all" -vault-path="/Users/johndoe/Obsidian\ Vault/Testing" -vault-folder="Migrated"`

### Download a single page and convert all page properties to frontmatter
`n2o -notion-token="NOTION_TOKEN" -notion-page-ID="1429989f-e8ac-4eff-bc8f-57f56486db54" -page-properties="all" -vault-path="/Users/johndoe/Obsidian\ Vault/Testing" -vault-folder="Migrated" -save-to-disk`

### Download a full database and images, and convert some page properties to frontmatter
`n2o -notion-token="NOTION_TOKEN" -notion-page-ID="668d797c-76fa-4934-9b05-ad288df2d136" -page-properties="location" -vault-path="/Users/johndoe/Obsidian\ Vault/Testing" -vault-folder="Migrated"` -download-images -save-to-disk

### Download a complete database and customize the resulting Obsidan page name
`n2o -notion-token="NOTION_TOKEN" -notion-page-ID="668d797c-76fa-4934-9b05-ad288df2d136" -page-name="date:%Y/%B/%d-%A" -vault-path="/Users/johndoe/Obsidian\ Vault/Testing" -vault-folder="Migrated" -download-images -save-to-disk`

For these examples, the location of the different pages would be based dynamically on the `date` value in the Notion page property and the custom format `%Y/%B/%d-%A`. For a list of available formatting options for dates, refer to [man 3 strftime](https://linux.die.net/man/3/strftime)

A notion page with the date value `2024-09-30` would be stored in: `/Users/johndoe/Obsidian\ Vault/Testing/Migrated/2024/September/09-Monday`

## Known Limitations

## TODOS

- [ ] Figure out how to parse self-referential links. Transform links like `/<Notion_PAGE_ID>#<BLOCK_ID>` to `[[Page^Block_ID]]` or `[[Page#Block_ID]]`
- [ ] Create Brew formula
