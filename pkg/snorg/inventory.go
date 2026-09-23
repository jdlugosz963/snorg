package snorg

import "github.com/jdlugosz963/snorg/internal/query"

// Keywords lists the archive's distinct device keywords with per-value page counts,
// sorted by value (the read side of the CLI's `list keywords`).
func (c *Client) Keywords() ([]ValueCount, error) { return query.Keywords(c.arch) }

// Tags lists the archive's distinct snorg-managed tags with per-value page counts,
// sorted by value (the read side of the CLI's `list tags`).
func (c *Client) Tags() ([]ValueCount, error) { return query.Tags(c.arch) }
