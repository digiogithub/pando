// Root barrel of @pando/client.
//
// Every module is also reachable by subpath (@pando/client/stores/sessionStore),
// which is what the core frontend uses. This barrel exists for consumers that
// prefer a single entry point, and as the one place that shows the package's
// whole public surface at a glance.

export * from './types'

// services
export * from './services/api'
export { default as api } from './services/api'
export * from './services/auth'
export * from './services/commandLauncher'
export * from './services/extensionUI'
export * from './services/host'
export * from './services/mappers'
export * from './services/sse'
export * from './services/terminalPty'

// stores
export * from './stores/agentVcsStore'
export * from './stores/configEventsStore'
export * from './stores/configInitStore'
export * from './stores/containerStore'
export * from './stores/cronJobsStore'
export * from './stores/editorStore'
export * from './stores/evaluatorStore'
export * from './stores/extensionPanelsStore'
export * from './stores/extensionsStore'
export * from './stores/fileChangesStore'
export * from './stores/instancesStore'
export * from './stores/layoutStore'
export * from './stores/logsStore'
export * from './stores/lspStore'
export * from './stores/mcpGatewayStore'
export * from './stores/mcpServersStore'
export * from './stores/notificationsStore'
export * from './stores/orchestratorStore'
export * from './stores/projectStore'
export * from './stores/serverStore'
export * from './stores/servicesSettingsStore'
export * from './stores/sessionStore'
export * from './stores/settingsStore'
export * from './stores/snapshotsStore'
export * from './stores/terminalStore'
export * from './stores/toastStore'

// hooks
export * from './hooks/useChat'
export * from './hooks/useGoal'

