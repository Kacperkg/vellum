import { Outlet, useLocation } from "@tanstack/react-router";
import SideNav from "../components/SideNav";
import Nav from "../components/Nav";


export default function RootLayout() {
  const { pathname } = useLocation()

  if (pathname === "/login") {
    return <Outlet />
  }

  return (
    <div className="flex min-h-screen">
      <SideNav />
      <main className="min-w-0 flex-1">
        <Nav/>
        <Outlet />
      </main>
    </div>
  )
}