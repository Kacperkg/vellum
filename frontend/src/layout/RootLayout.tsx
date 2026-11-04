import { useEffect, useSyncExternalStore } from "react";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  getSession,
  getSessionSnapshot,
  subscribeAuth,
} from "@/api/tokenStorage";
import SideNav from "./components/SideNav";
import Nav from "./components/Nav";
import { FinanceDataProvider } from "@/contexts/FinanceDataContext";
import AccountSetupGate from "@/features/accounts/components/AccountSetupGate";

export default function RootLayout() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  useSyncExternalStore(subscribeAuth, getSessionSnapshot);
  const session = getSession();
  const publicRoute = [
    "/login",
    "/forgot-password",
    "/reset-password",
  ].includes(pathname);
  const authenticated = !!session?.credentials;

  useEffect(() => {
    if (!publicRoute && !authenticated)
      void navigate({ to: "/login", replace: true });
  }, [authenticated, navigate, publicRoute]);

  if (publicRoute) {
    return <Outlet />;
  }

  if (!authenticated) return null;

  return (
    <FinanceDataProvider key={session.generation}>
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
