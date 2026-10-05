package projectknowledge

import pm "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/projectmanagement"

func (r WriteRequest) validateActionFields() error {
	fields := ""
	switch r.Action {
	case "space.ensure", "space.create", "space.update":
		fields = "title body fields"
	case "content.create":
		fields = "title body contentKind fields labels template"
	case "content.update":
		fields = "title body fields clear"
	case "draft.publish":
		fields = "versionId"
	case "content.move", "content.copy":
		fields = "move title"
	case "version.restore":
		fields = "versionId"
	case "comment.create", "comment.update":
		fields = "comment"
	case "reaction.add", "reaction.remove":
		fields = "reaction"
	case "watcher.add", "watcher.remove":
		fields = "principalId"
	case "inline-task.update":
		fields = "inlineTask"
	case "labels.set":
		fields = "labels"
	case "properties.set":
		fields = "fields clear"
	case "template.apply":
		fields = "template fields"
	case "template.create", "template.update":
		fields = "title body fields"
	case "state.set":
		fields = "state"
	case "permissions.set":
		fields = "grants"
	case "export":
		fields = "export"
	}
	return pm.ActionFields(r, fields)
}
