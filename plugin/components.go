package plugin

import (
	"encoding/json"
	"fmt"
)

const ComponentAvailableCommand = "components.available.v1"
const ComponentAssignCommand = "components.assign.v1"
const ComponentContributeCommand = "components.contribute.v1"

const ComponentSnapshotCommand = "components.snapshot.v1"
const ComponentInvokeCommand = "components.invoke.v1"
const ComponentChangedEvent = "components.changed.v1"

// ComponentIdentity addresses one mounted instance, never the implicitly selected session.
// Identity is a target, not authority: hosts must separately validate owner-scoped grants.
type ComponentIdentity struct {
	ID         string `json:"id" bd:"subject"`
	Family     string `json:"family" bd:"public"`
	OwnerID    string `json:"ownerId" bd:"public"`
	Generation string `json:"generation" bd:"public"`
	ProjectID  string `json:"projectId,omitempty" bd:"subject"`
	SessionID  string `json:"sessionId,omitempty" bd:"subject"`
}

// ComponentExtension is additive. It cannot replace rendering or expose terminal I/O.
type ComponentExtension struct {
	OwnerID    string            `json:"ownerId" bd:"public"`
	Generation string            `json:"generation" bd:"public"`
	PanelID    string            `json:"panelId,omitempty" bd:"public"`
	Icon       string            `json:"icon,omitempty" bd:"public"`
	ID         string            `json:"id" bd:"public"`
	Target     ComponentIdentity `json:"target" bd:"subject"`
	Point      string            `json:"point" bd:"public"`
	Label      string            `json:"label" bd:"public"`
	Order      int               `json:"order" bd:"public"`
	Command    string            `json:"command,omitempty" bd:"public"`
}

func ValidateComponentIdentity(v ComponentIdentity) error {
	if v.ID == "" || v.OwnerID == "" || v.Generation == "" {
		return fmt.Errorf("component id, owner and generation required")
	}
	switch v.Family {
	case "workspace", "session-list", "session-tab", "terminal", "stage", "tasks", "file-tree", "project-tools":
		return nil
	}
	return fmt.Errorf("unsupported component family %q", v.Family)
}
func ValidateComponentExtension(v ComponentExtension) error {
	if err := ValidateComponentIdentity(v.Target); err != nil {
		return err
	}
	if v.ID == "" || v.Label == "" {
		return fmt.Errorf("extension id and accessible label required")
	}
	switch v.Point {
	case "badge", "metadata", "action", "menu", "panel":
		return nil
	}
	return fmt.Errorf("unsupported component extension point %q", v.Point)
}

type ComponentAssignment struct {
	Identity ComponentIdentity `json:"identity" bd:"subject"`
	Lease    string            `json:"lease" bd:"subject"`
}

type ComponentWorkspaceSnapshot struct {
	Ready    bool                `json:"ready" bd:"subject"`
	Children []ComponentIdentity `json:"children" bd:"subject"`
}

type ComponentSessionListSnapshot struct {
	SessionIDs []string `json:"sessionIds" bd:"subject"`
	SelectedID string   `json:"selectedId" bd:"subject"`
	Group      string   `json:"group" bd:"subject"`
}

type ComponentSessionTabSnapshot struct {
	Title    string `json:"title" bd:"subject"`
	Provider string `json:"provider" bd:"subject"`
	Status   string `json:"status" bd:"subject"`
	Pinned   bool   `json:"pinned" bd:"subject"`
}

type ComponentTerminalSnapshot struct {
	Connected bool   `json:"connected" bd:"subject"`
	Visible   bool   `json:"visible" bd:"subject"`
	ViewMode  string `json:"viewMode" bd:"subject"`
}

type ComponentPlacement struct {
	SessionID     string `json:"sessionId" bd:"subject"`
	Column        int    `json:"column" bd:"subject"`
	Row           int    `json:"row" bd:"subject"`
	Width         int    `json:"width" bd:"subject"`
	Height        int    `json:"height" bd:"subject"`
	DisplayWidth  int    `json:"displayWidth" bd:"subject"`
	DisplayHeight int    `json:"displayHeight" bd:"subject"`
}

type ComponentStageSnapshot struct {
	Placements []ComponentPlacement `json:"placements" bd:"subject"`
	SaveStatus string               `json:"saveStatus" bd:"subject"`
}

type ComponentTasksSnapshot struct {
	Available bool   `json:"available" bd:"subject"`
	Running   bool   `json:"running" bd:"subject"`
	Href      string `json:"href" bd:"subject"`
	Starting  bool   `json:"starting" bd:"subject"`
	Error     string `json:"error" bd:"subject"`
}

type ComponentFileTreeSnapshot struct {
	Root          string   `json:"root" bd:"subject"`
	SelectedPath  string   `json:"selectedPath" bd:"subject"`
	ExpandedPaths []string `json:"expandedPaths" bd:"subject"`
	Filter        string   `json:"filter" bd:"subject"`
	Loading       bool     `json:"loading" bd:"subject"`
}

type ComponentProjectToolsSnapshot struct {
	Views        []string `json:"views" bd:"subject"`
	SelectedView string   `json:"selectedView" bd:"subject"`
	Expanded     bool     `json:"expanded" bd:"subject"`
	Loading      bool     `json:"loading" bd:"subject"`
}

// Assignment requests name an instance; the host checks explicit operator consent.
type ComponentAvailableRequest struct{}
type ComponentAvailableResult struct {
	Components []ComponentIdentity `json:"components" bd:"subject"`
}
type ComponentAssignRequest struct {
	Identity ComponentIdentity `json:"identity" bd:"subject"`
}
type ComponentAssignResult struct {
	Assignment ComponentAssignment `json:"assignment" bd:"subject"`
}
type ComponentContributeRequest struct {
	Assignment ComponentAssignment `json:"assignment" bd:"subject"`
	Extension  ComponentExtension  `json:"extension" bd:"subject"`
}
type ComponentContributeResult struct {
	Extension ComponentExtension `json:"extension" bd:"subject"`
}

// ComponentMethods returns a fresh capability vocabulary; instances advertise only implemented methods.
func ComponentMethods(family string) []string {
	switch family {
	case "workspace":
		return []string{"discover"}
	case "session-list":
		return []string{"select", "reorder", "group", "launch"}
	case "session-tab":
		return []string{"select", "rename", "pin", "close", "sleep", "wake"}
	case "terminal":
		return []string{"focus", "reconnect", "setViewMode"}
	case "stage":
		return []string{"move", "resize", "focus", "restore"}
	case "tasks":
		return []string{"refresh", "start"}
	case "file-tree":
		return []string{"select", "expand", "filter", "refresh", "createFile", "createFolder", "rename", "remove"}
	case "project-tools":
		return []string{"selectView", "refresh", "setExpanded"}
	}
	return nil
}

// Raw JSON occurs only at the transport envelope; named client methods and family snapshots remain typed.
type ComponentSnapshotRequest struct {
	Assignment ComponentAssignment `json:"assignment" bd:"subject"`
}
type ComponentSnapshotResult struct {
	// Revision is host-owned and monotonic within one component incarnation.
	Revision     uint64            `json:"revision" bd:"public"`
	Identity     ComponentIdentity `json:"identity" bd:"subject"`
	Snapshot     json.RawMessage   `json:"snapshot" bd:"subject"`
	Capabilities []string          `json:"capabilities" bd:"public"`
}
type ComponentInvokeRequest struct {
	Assignment ComponentAssignment `json:"assignment" bd:"subject"`
	Method     string              `json:"method" bd:"public"`
	Args       []json.RawMessage   `json:"args" bd:"subject"`
}
type ComponentChanged struct {
	Revision  uint64            `json:"revision" bd:"public"`
	Identity  ComponentIdentity `json:"identity" bd:"subject"`
	Lease     string            `json:"lease" bd:"subject"`
	Snapshot  json.RawMessage   `json:"snapshot,omitempty" bd:"subject"`
	Withdrawn bool              `json:"withdrawn,omitempty" bd:"public"`
}
