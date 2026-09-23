import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import './styles.css'

const container = document.getElementById('root')
if (container === null) {
  // index.html is served from the same build as this script, so this can only
  // happen if someone mounts the bundle in a page of their own.
  throw new Error('no #root element to mount the guide in')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
