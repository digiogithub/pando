import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Suspense } from 'react'
import MainLayout from '@/components/layout/MainLayout'
import LoadingSpinner from '@/components/shared/LoadingSpinner'

// Placeholder pages for routes (will be replaced in Phase 2+)
function PlaceholderPage({ name }: { name: string }) {
  return (
    <div style={{ padding: '2rem', color: 'var(--fg)' }}>
      <h2 style={{ fontSize: 20, fontWeight: 600, marginBottom: '0.5rem' }}>{name}</h2>
      <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>This view will be implemented soon.</p>
    </div>
  )
}

function App() {
  return (
    <BrowserRouter>
      <Suspense fallback={<div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}><LoadingSpinner size={32} /></div>}>
        <Routes>
          <Route path="/" element={<MainLayout />}>
            <Route index element={<PlaceholderPage name="Chat" />} />
            <Route path="chat/simple" element={<PlaceholderPage name="Simple Chat" />} />
            <Route path="orchestrator" element={<PlaceholderPage name="Orchestrator" />} />
            <Route path="logs" element={<PlaceholderPage name="Logs" />} />
            <Route path="snapshots" element={<PlaceholderPage name="Snapshots" />} />
            <Route path="evaluator" element={<PlaceholderPage name="Evaluator" />} />
            <Route path="editor" element={<PlaceholderPage name="Code Editor" />} />
            <Route path="terminal" element={<PlaceholderPage name="Terminal" />} />
            <Route path="settings" element={<PlaceholderPage name="Settings" />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </Suspense>
    </BrowserRouter>
  )
}

export default App
