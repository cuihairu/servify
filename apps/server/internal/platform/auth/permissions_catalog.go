package auth

const (
	ResourceCustomers       = "customers"
	ResourceAgents          = "agents"
	ResourceTickets         = "tickets"
	ResourceSessionTransfer = "session_transfer"
	ResourceSatisfaction    = "satisfaction"
	ResourceWorkspace       = "workspace"
	ResourceMacros          = "macros"
	ResourceIntegrations    = "integrations"
	ResourceCustomFields    = "custom_fields"
	ResourceStatistics      = "statistics"
	ResourceSLA             = "sla"
	ResourceShift           = "shift"
	ResourceAutomation      = "automation"
	ResourceKnowledge       = "knowledge"
	ResourceAssist          = "assist"
	ResourceGamification    = "gamification"
	ResourceVoice           = "voice"
)

var fallbackAgentPermissions = []string{
	ResourcePermission(ResourceTickets, "GET"),
	ResourcePermission(ResourceTickets, "POST"),
	ResourcePermission(ResourceCustomers, "GET"),
	ResourcePermission(ResourceAgents, "GET"),
	ResourcePermission(ResourceCustomFields, "GET"),
	ResourcePermission(ResourceSessionTransfer, "GET"),
	ResourcePermission(ResourceSessionTransfer, "POST"),
	ResourcePermission(ResourceSatisfaction, "GET"),
	ResourcePermission(ResourceSatisfaction, "POST"),
	ResourcePermission(ResourceWorkspace, "GET"),
	ResourcePermission(ResourceMacros, "GET"),
	ResourcePermission(ResourceIntegrations, "GET"),
	ResourcePermission(ResourceVoice, "GET"),
	ResourcePermission(ResourceVoice, "POST"),
}
