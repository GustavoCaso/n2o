## Notion To Obsidian migrator (n2o)

Migration tool for your Notion workspace to Obsidian

### Install

To install the binary you could:
- Use homebrew `brew install n2o`
- Download the `n2o` tool from releases page.

### Usage

```
$ n2o
Usage of n2o:
   -download-images
    	download external images to Obsidan vault
  -notion-db-ID string
    	Notion database to migrate
  -notion-page-ID string
    	Notion page to migrate
  -notion-token string
    	Notion token
  -page-name string
    	Notion page properties to extract the Obsidian page title.
    	Support selecting different page attribute and formatting. To select multiple properties, use a comma separate list.
    	The attribute that support custom formatting are Notion date attributes.
    	Example of how to use a Notion date property with custom format as the title for the Obsidian page:
    	-name=date:%Y/%B/%d-%A
    	 (default "Name")
  -page-properties string
    	Notion page properties to convert to Obsidian frontmater.
    	You can use select multiple properties, use a comma separate list.

  -vault-folder string
    	folder to store pages inside Obsidian Vault
  -vault-path string
    	Obsidian vault location
```

#### How to get your Notion token?

To get your Notion token you either have to create an integration or use an existing integration that has access to the workspace you want to migrate. There are a lot of resources in the Notion website.

- [What is an Integration](https://developers.notion.com/docs/getting-started#what-is-a-notion-integration)
- [Build your first integration Guide](https://developers.notion.com/docs/create-a-notion-integration)

Once you have your integration installed in your workspace, we can start migrating single pages or full databases to our Obsidian vault.

#### How to get the database ID?

Copied from the Notion website: [Retrieve a database](https://developers.notion.com/reference/retrieve-a-database)

> To find a database ID, navigate to the database URL in your Notion workspace. The ID is the string of characters in the URL that is between the slash following the workspace name (if applicable) and the question mark. The ID is a 32 characters alphanumeric string.

![databaseID](img/databaseID.png)

#### How to retrieve the Page ID?

Copied from the Notion website: [Working with page content](https://developers.notion.com/docs/working-with-page-content)
> Here's a quick procedure to find the page ID for a specific page in Notion: 
Open the page in Notion. Use the Share menu to Copy link. Now paste the link in your text editor so you can take a closer look. The URL ends in a page ID.
It should be a 32 character long string. Format this value by inserting hyphens (-) in the following pattern: 8-4-4-4-12 (each number is the length of characters between the hyphens).
Example: 1429989fe8ac4effbc8f57f56486db54 becomes 1429989f-e8ac-4eff-bc8f-57f56486db54.
This value is your page ID.



TODOS:
- [ ] Figure out how to parse self-referential links. Transform links like `/<Notion_PAGE_ID>#<BLOCK_ID>` to `[[Page^Block_ID]]` or `[[Page#Block_ID]]`
- [ ] Better error handling
- [ ] Use better logging
- [ ] Create Brew formula
- [ ] Create Github Action to create a release
