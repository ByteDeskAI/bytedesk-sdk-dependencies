package projecttasks

import pm "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/projectmanagement"

func (r WriteRequest) validateActionFields() error {
	fields := ""
	switch r.Action {
	case "project.ensure", "project.create", "project.update":
		fields = "title body fields"
	case "item.create":
		fields = "title body fields resourceKind assigneeId"
	case "item.patch", "item.replace":
		fields = "title body fields clear assigneeId"
	case "item.assign":
		fields = "assigneeId"
	case "item.clone":
		fields = "title body fields"
	case "item.bulk":
		fields = "items bulkAction fields clear assigneeId actionId"
	case "workflow.transition":
		fields = "actionId fields"
	case "relation.add", "relation.remove":
		fields = "relation"
	case "comment.create", "comment.update":
		fields = "comment"
	case "watcher.add", "watcher.remove":
		fields = "principalId"
	case "worklog.create", "worklog.update":
		fields = "worklog"
	case "planning.create", "planning.update":
		fields = "planning fields clear"
	case "planning.transition":
		fields = "actionId fields"
	case "planning.membership":
		fields = "items"
	case "planning.rank":
		fields = "position items"
	case "agent.update", "agent.dispatch", "agent.review":
		fields = "agent"
	case "agent.complete":
		fields = "agent leaseFence"
	case "native.sync":
		fields = "native title body fields clear"
	case "admin.fields":
		fields = "fieldDefinitions"
	case "admin.workflow":
		fields = "workflowActions"
	case "admin.screens":
		fields = "fields"
	case "admin.permissions":
		fields = "grants"
	}
	return pm.ActionFields(r, fields)
}
