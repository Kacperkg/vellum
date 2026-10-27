import { Outlet, useLocation } from "@tanstack/react-router";
import SideNav from "./components/SideNav";
import Nav from "./components/Nav";
import { FinanceDataProvider } from "@/contexts/FinanceDataContext";
import AccountSetupGate from "@/features/accounts/components/AccountSetupGate";

export default function RootLayout() {
  const { pathname } = useLocation();

  if (["/login", "/forgot-password", "/reset-password"].includes(pathname)) {
    return <Outlet />;
  }

  return (
    <FinanceDataProvider>
      <AccountSetupGate>
        <div className="app-frame">
          <SideNav />
          <main className="app-main">
            <Nav />
            <Outlet />
          </main>
        </div>
      </AccountSetupGate>
    </FinanceDataProvider>
  );
}
