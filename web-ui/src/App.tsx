import { BrowserRouter, Routes, Route } from 'react-router-dom'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<div style={{ padding: '2rem', color: 'var(--fg)' }}>Pando Web UI — Loading...</div>} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
