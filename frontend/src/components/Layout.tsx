import { Link } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export default function Layout({ children }: { children: React.ReactNode }) {
  const { user, logout } = useAuth()

  return (
    <div className="layout">
      <nav className="navbar">
        <Link to="/" className="navbar-brand">
          FarmSense
        </Link>
        <div className="navbar-right">
          {user && (
            <>
              <span className="navbar-user">{user.name}</span>
              <button className="btn btn-ghost" onClick={logout}>
                Sign out
              </button>
            </>
          )}
        </div>
      </nav>
      <main className="main-content">{children}</main>
    </div>
  )
}
