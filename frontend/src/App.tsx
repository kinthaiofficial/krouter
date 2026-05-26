import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Dashboard from './pages/Dashboard'
import FreeTokens from './pages/FreeTokens'
import Router from './pages/Router'
import Apps from './pages/Apps'
import Logs from './pages/Logs'
import Providers from './pages/Providers'
import BudgetPage from './pages/Budget'
import Settings from './pages/Settings'
import Announcements from './pages/Announcements'
import About from './pages/About'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 10_000 },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename="/krouter">
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="free-tokens" element={<FreeTokens />} />
            <Route path="router" element={<Router />} />
            <Route path="agents" element={<Apps />} />
            <Route path="logs" element={<Logs />} />
            <Route path="providers" element={<Providers />} />
            <Route path="budget" element={<BudgetPage />} />
            <Route path="settings" element={<Settings />} />
            <Route path="announcements" element={<Announcements />} />
            <Route path="about" element={<About />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
