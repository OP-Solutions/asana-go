package asana

import "fmt"

type SectionBase struct {
	WithName
}

// A Section is a subdivision of a project that groups tasks together. It can either be
// a header above a list of tasks in a list view or a column in a board view of a project.
//
// Sections are largely a shared idiom in Asana’s API for both list and board views of
// a project regardless of the project’s layout.
//
// The ‘memberships’ property when getting a task will return the information for the
// section or the column under ‘section’ in the response.
type Section struct {
	Expandable
	SectionBase
	WithCreated

	// Read-only. The project which contains the section.
	Project string `json:"project,omitempty"`
}

// Section creates an unexpanded Section object with the given ID
func (c *Client) Section(id string) *Section {
	result := &Section{}
	result.ID = id
	return result
}

// Expand loads the full details for this Section
func (s *Section) Expand(client *Client) error {
	client.trace("Loading section details for %q", s.Name)

	if s.expanded {
		return nil
	}

	_, err := client.get(fmt.Sprintf("/sections/%s", s.ID), nil, s)
	return err
}

// Sections returns a list of sections in this project
func (p *Project) Sections(client *Client, opts ...*Options) ([]*Section, *NextPage, error) {
	client.trace("Listing sections in %q", p.Name)
	var result []*Section

	// Make the request
	nextPage, err := client.get(fmt.Sprintf("/projects/%s/sections", p.ID), nil, &result, opts...)
	return result, nextPage, err
}

// CreateSection creates a new section in the given project
func (p *Project) CreateSection(client *Client, section *SectionBase) (*Section, error) {
	client.info("Creating section %q", section.Name)

	result := &Section{}
	result.expanded = true

	err := client.post(fmt.Sprintf("projects/%s/sections", p.ID), section, result)
	return result, err
}

type SectionInsertRequest struct {
	Project       string `json:"project_gid"`
	Section       string `json:"section"`
	BeforeSection string `json:"before_section,omitempty"`
	AfterSection  string `json:"after_section,omitempty"`
}

// InsertSection moves sections relative to each other in a board view.
// One of before_section or after_section is required.
//
// Sections cannot be moved between projects.
//
// At this point in time, moving sections is not supported in list views, only board views.
func (p *Project) InsertSection(client *Client, request *SectionInsertRequest) error {
	client.info("Moving section %s", request.Section)

	err := client.post(fmt.Sprintf("projects/%s/sections/insert", p.ID), request, nil)
	return err
}
