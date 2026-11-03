import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './nse.css'
import NSEDashboard from './NSEDashboard'

createRoot(document.getElementById('nse-root')!).render(
  <StrictMode>
    <NSEDashboard />
  </StrictMode>,
)
