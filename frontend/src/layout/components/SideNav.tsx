import { useState } from "react";
import Logo from "@/components/shared/Logo";
import { Link } from "@tanstack/react-router";
import Card, {
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card";
import Button from "@/components/ui/Button";
import { logout } from "@/api/endpoints/auth";
import Icon, { type IconName } from "@/components/ui/Icon";
import Progress from "@/components/ui/Progress";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import {
  formatMoney,
  isCurrentMonth,
  moneyInteger,
  moneyRatio,
} from "@/utils/finance";

const routes = [
  { label: "Overview", path: "/", icon: "overview" },
  { label: "Accounts", path: "/accounts", icon: "accounts" },
  { label: "Transactions", path: "/transactions", icon: "transactions" },
  { label: "Budgets", path: "/budgets", icon: "budgets" },
  { label: "Analytics", path: "/analytics", icon: "analytics" },
  { label: "Goals", path: "/goals", icon: "goals" },
  { label: "Subscriptions", path: "/subscriptions", icon: "subscriptions" },
  { label: "Settings", path: "/settings", icon: "settings" },
] as const;

export default function SideNav() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const { budgets, transactions, settings } = useFinanceData();
  const allocated = budgets.reduce(
    (sum, budget) => sum + moneyInteger(budget.allocated_amount),
    0n,
  );
  const budgetCategoryIds = new Set(
    budgets.map((budget) => budget.category_id),
  );
  const spent = transactions
    .filter(
      (transaction) =>
        transaction.type === "expense" &&
        isCurrentMonth(transaction.date) &&
        budgetCategoryIds.has(transaction.category_id),
    )
    .reduce((sum, transaction) => sum + moneyInteger(transaction.amount), 0n);
  const utilisation = allocated > 0 ? moneyRatio(spent, allocated) * 100 : 0;
  const month = new Intl.DateTimeFormat("en", { month: "long" }).format(
    new Date(),
  );

  async function handleLogout(): Promise<void> {
    await logout();
  }

  return (
    <>
      <button
        type="button"
        className="mobile-menu-toggle"
        aria-label="Open workspace navigation"
        aria-expanded={mobileOpen}
        onClick={() => setMobileOpen(true)}
      >
        <span />
        <span />
        <span />
      </button>
      {mobileOpen && (
        <button
          className="mobile-nav-scrim"
          aria-label="Close workspace navigation"
          onClick={() => setMobileOpen(false)}
        />
      )}
      <aside className={`side-nav ${mobileOpen ? "mobile-open" : ""}`}>
        <div>
          <Logo />
          <div className="side-nav-links">
            <h2>Workspace</h2>
            {routes.map((item) => (
              <Link
                key={item.path}
                to={item.path}
                onClick={() => setMobileOpen(false)}
                activeProps={{
                  className: "active",
                }}
                className="side-nav-link"
              >
                <Icon name={item.icon as IconName} />
                {item.label}
              </Link>
            ))}
          </div>
        </div>
        <div className="side-nav-bottom">
          {budgets.length > 0 && (
            <Card className="month-card">
              <CardHeader>
                <CardTitle>{month}</CardTitle>
              </CardHeader>
              <CardContent>
                <strong>{settings ? formatMoney(spent, settings) : "—"}</strong>
                <p>
                  {settings
                    ? `of ${formatMoney(allocated, settings)} budget`
                    : "Loading budget"}
                </p>
              </CardContent>
              <CardFooter>
                <Progress value={utilisation} />
              </CardFooter>
            </Card>
          )}
          <Button variant="ghost" className="sign-out" onClick={handleLogout}>
            <Icon name="logout" />
            <span>Sign out</span>
          </Button>
        </div>
      </aside>
    </>
  );
}
