// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package changepub

// MethodToTopic maps a db.write method name to the corresponding PUB topic.
// Returns an empty string for methods that do not warrant a change event
// (e.g. read-only helpers or internal bookkeeping that secondaries need not see).
func MethodToTopic(method string) string {
	switch method {
	case "CreateSession":
		return "db.session.created"
	case "UpdateSession":
		return "db.session.updated"
	case "DeleteSession":
		return "db.session.deleted"
	case "CreateMessage":
		return "db.message.created"
	case "UpdateMessage":
		return "db.message.updated"
	case "DeleteMessage":
		return "db.message.deleted"
	case "CreateFile":
		return "db.file.created"
	case "DeleteFile", "DeleteSessionFiles":
		return "db.file.deleted"
	case "CreateProject":
		return "db.project.created"
	case "UpdateProjectStatus", "UpdateProjectLastOpened", "MarkProjectInitialized":
		return "db.project.updated"
	case "DeleteProject":
		return "db.project.deleted"
	case "InsertSkill":
		return "db.skill.created"
	case "IncrementSkillUsage", "DeactivateLowestSkill":
		return "db.skill.updated"
	default:
		return ""
	}
}
