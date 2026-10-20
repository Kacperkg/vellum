import { Outlet, useLocation } from "@tanstack/react-router";
import SideNav from "../components/SideNav";
import Nav from "../components/Nav";
import { FinanceDataProvider } from "../context/FinanceDataContext";

export default function RootLayout() {
  const { pathname } = useLocation();

  if (pathname === "/login") {
    return <Outlet />;
  }

  return (
    <FinanceDataProvider>
      <div className="app-frame">
        <SideNav />
        <main className="app-main">
          <Nav />
          <Outlet />
        </main>
      </div>
    </FinanceDataProvider>
  );
}
