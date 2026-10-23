import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import Card from "../components/Card";
import DataState from "../components/DataState";
import Icon from "../components/Icon";
import PageHeader from "../components/PageHeader";
import Progress from "../components/Progress";
import { useFinanceData } from "../context/FinanceDataContext";
import { formatMoney, isCurrentMonth, netAmount } from "../lib/finance";
import { financialActivity, formatActivityAmount } from "../lib/activity";

function sparkline(values: number[]): string {
  if (values.length === 0) return "0,70 700,70";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = Math.max(max - min, 1);
  return values
    .map((value, index) => {
      const x = (index / Math.max(values.length - 1, 1)) * 700;
      const y = 20 + ((max - value) / range) * 85;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

export default function Home() {
  const {
    accounts,
    budgets,
    categories,
    transactions,
    transfers,
    settings,
    user,
    loading,
    errors,
    refresh,
  } = useFinanceData();
  const error =
    errors.accounts ??
    errors.budgets ??
    errors.categories ??
    errors.transactions ??
    errors.transfers ??
    errors.settings;
  const [now] = useState(() => new Date());
  const categoryById = useMemo(
    () => new Map(categories.map((item) => [item.id, item.name])),
    [categories],
  );
  const currentMonth = transactions.filter((item) => isCurrentMonth(item.date));
  const income = currentMonth
    .filter((item) => item.type === "income")
    .reduce((sum, item) => sum + item.amount, 0);
  const expenses = currentMonth
    .filter((item) => item.type === "expense")
    .reduce((sum, item) => sum + item.amount, 0);
  const allocated = budgets.reduce(
    (sum, item) => sum + item.allocated_amount,
    0,
  );
  const budgetCategoryIds = new Set(budgets.map((item) => item.category_id));
  const budgetedExpenses = currentMonth
    .filter(
      (item) =>
        item.type === "expense" && budgetCategoryIds.has(item.category_id),
    )
    .reduce((sum, item) => sum + item.amount, 0);
  const remaining = Math.max(allocated - budgetedExpenses, 0);
  const daysInMonth = new Date(
    now.getFullYear(),
    now.getMonth() + 1,
    0,
  ).getDate();
  const projectedSpend =
    now.getDate() > 0
      ? Math.round((budgetedExpenses / now.getDate()) * daysInMonth)
      : budgetedExpenses;
  const underBudget = Math.max(allocated - projectedSpend, 0);
  const utilisation = allocated > 0 ? (budgetedExpenses / allocated) * 100 : 0;
  const weekStart = now.getTime() - 7 * 24 * 60 * 60 * 1000;
  const weeklyChange = netAmount(
    transactions.filter((item) => new Date(item.date).getTime() >= weekStart),
  );
  const recent = financialActivity(transactions, transfers).slice(0, 6);
  const spendByCategory = new Map<string, number>();
  currentMonth
    .filter((item) => item.type === "expense")
    .forEach((item) =>
      spendByCategory.set(
        item.category_id,
        (spendByCategory.get(item.category_id) ?? 0) + item.amount,
      ),
    );
  const budgetRows = budgets
    .map((budget) => ({
      ...budget,
      name: categoryById.get(budget.category_id) ?? "Uncategorised",
      spent: spendByCategory.get(budget.category_id) ?? 0,
    }))
    .sort((a, b) => b.spent - a.spent)
    .slice(0, 4);
  const topCategory = [...spendByCategory.entries()].sort(
    (a, b) => b[1] - a[1],
  )[0];
  const recurring = transactions.filter(
    (item) => item.type === "expense" && item.frequency !== "one-off",
  );
  const monthLabel = new Intl.DateTimeFormat("en", {
    month: "long",
    year: "numeric",
  }).format(now);
  const balanceValues = [...currentMonth]
    .sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime())
    .reduce<{ total: number; values: number[] }>(
      (result, item) => {
        const total =
          result.total + (item.type === "income" ? item.amount : -item.amount);
        return { total, values: [...result.values, total] };
      },
      { total: 0, values: [] },
    ).values;
  const balancePoints = sparkline(balanceValues);

  return (
    <div className="page-shell overview-page">
      <PageHeader
        eyebrow={monthLabel}
        title={`Good ${now.getHours() < 12 ? "morning" : now.getHours() < 18 ? "afternoon" : "evening"}, ${user?.first_name ?? "there"}.`}
        description="A calm overview of where your money sits today."
      />
      <DataState
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
      />
      {!loading && !error && settings && (
        <>
          <section className="overview-hero-grid">
            <Card className="balance-card">
              <div className="balance-head">
                <div>
                  <p className="metric-label">Current net balance</p>
                  <p className="balance-amount">
                    {formatMoney(netAmount(transactions), settings)}
                  </p>
                  <p className="balance-note">
                    <span>
                      <Icon name="trend" />{" "}
                      {formatMoney(weeklyChange, settings)} this week
                    </span>
                    <i />
                    Across {accounts.length} accounts
                  </p>
                </div>
                <div className="balance-summaries">
                  <div>
                    <p className="metric-label">Income</p>
                    <strong className="positive">
                      <Icon name="trend" />
                      {formatMoney(income, settings, 0)}
                    </strong>
                  </div>
                  <div>
                    <p className="metric-label">Expenses</p>
                    <strong>
                      <Icon name="trend" className="rotate-90" />
                      {formatMoney(expenses, settings, 0)}
                    </strong>
                  </div>
                </div>
              </div>
              <svg
                className="balance-chart"
                viewBox="0 0 700 130"
                preserveAspectRatio="none"
                aria-label="Live balance activity"
              >
                <defs>
                  <linearGradient id="balance-fill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0" stopColor="#e2416c" stopOpacity=".22" />
                    <stop offset="1" stopColor="#e2416c" stopOpacity=".02" />
                  </linearGradient>
                </defs>
                <polygon
                  points={`0,130 ${balancePoints} 700,130`}
                  fill="url(#balance-fill)"
                />
                <polyline
                  points={balancePoints}
                  fill="none"
                  stroke="#e2416c"
                  strokeWidth="1.5"
                />
              </svg>
            </Card>
            <Card className="remaining-card">
              <p className="metric-label">Remaining to spend</p>
              <strong>{formatMoney(remaining, settings)}</strong>
              <p>of {formatMoney(allocated, settings)} for the month</p>
              <div className="remaining-progress">
                <Progress value={utilisation} />
                <div>
                  <span>{Math.round(utilisation)}% spent</span>
                  <span>
                    {Math.max(daysInMonth - now.getDate(), 0)} days left
                  </span>
                </div>
              </div>
              <div className="forecast-card">
                <p>
                  <Icon name="spark" />
                  Pace forecast
                </p>
                <strong>
                  ~{formatMoney(projectedSpend, settings)} projected
                </strong>
                <span>
                  {underBudget > 0
                    ? `${formatMoney(underBudget, settings)} under budget`
                    : "Projected to exceed the current budget"}
                  .
                </span>
              </div>
            </Card>
          </section>
          <section className="insight-grid">
            <Card className="insight-card">
              <div>
                <span className="pill-label">On pace</span>
                <Icon name="arrow" />
              </div>
              <h2>
                {remaining > 0
                  ? "You still have room this month"
                  : "Monthly spending reached its allocation"}
              </h2>
              <p>
                {allocated > 0
                  ? `${formatMoney(remaining, settings)} remains across your current category budgets.`
                  : "Create a budget to start tracking spending pace."}
              </p>
            </Card>
            <Card className="insight-card">
              <div>
                <span className="pill-label">Pattern</span>
                <Icon name="arrow" />
              </div>
              <h2>
                {topCategory
                  ? `${categoryById.get(topCategory[0]) ?? "Top category"} leads spending`
                  : "No spending pattern yet"}
              </h2>
              <p>
                {topCategory
                  ? `${formatMoney(topCategory[1], settings)} recorded in this category this month.`
                  : "Add expenses to reveal your strongest category patterns."}
              </p>
            </Card>
            <Card className="insight-card">
              <div>
                <span className="pill-label">Recurring</span>
                <Icon name="arrow" />
              </div>
              <h2>{recurring.length} recurring expenses tracked</h2>
              <p>
                {recurring.length
                  ? `${formatMoney(
                      recurring.reduce((sum, item) => sum + item.amount, 0),
                      settings,
                    )} across recorded recurring expenses.`
                  : "Mark a transaction as recurring to see it here."}
              </p>
            </Card>
          </section>
          <section className="overview-lower-grid">
            <Card className="section-card">
              <div className="section-card-header">
                <div>
                  <h2>Recent activity</h2>
                  <p>Latest {recent.length} entries</p>
                </div>
                <Link to="/transactions">View all →</Link>
              </div>
              {recent.length === 0 ? (
                <p className="overview-total">No activity yet.</p>
              ) : (
                <div className="activity-list">
                  {recent.map((transaction) => (
                    <div className="activity-row" key={transaction.id}>
                      <span className="merchant-mark">
                        {transaction.name[0]?.toUpperCase()}
                      </span>
                      <div>
                        <strong>{transaction.name}</strong>
                        <p>
                          {transaction.type === "transfer"
                            ? `${accounts.find((account) => account.id === transaction.from_account_id)?.name ?? "Account"} → ${accounts.find((account) => account.id === transaction.to_account_id)?.name ?? "Account"}`
                            : (categoryById.get(transaction.category_id) ??
                              "Uncategorised")}
                        </p>
                      </div>
                      <strong
                        className={
                          transaction.type === "income" ? "positive" : ""
                        }
                      >
                        {formatActivityAmount(transaction, settings)}
                      </strong>
                    </div>
                  ))}
                </div>
              )}
            </Card>
            <Card className="section-card">
              <div className="section-card-header">
                <div>
                  <h2>Budgets in motion</h2>
                  <p>Top categories this month</p>
                </div>
                <Link to="/budgets">Manage →</Link>
              </div>
              {budgetRows.length === 0 ? (
                <p className="overview-total">No budgets yet.</p>
              ) : (
                <div className="mini-budget-list">
                  {budgetRows.map((budget) => (
                    <div key={budget.id}>
                      <div>
                        <strong>{budget.name}</strong>
                        <span>
                          {formatMoney(budget.spent, settings)} /{" "}
                          {formatMoney(budget.allocated_amount, settings)}
                        </span>
                      </div>
                      <Progress
                        value={
                          budget.allocated_amount > 0
                            ? (budget.spent / budget.allocated_amount) * 100
                            : 0
                        }
                        tone="neutral"
                      />
                    </div>
                  ))}
                </div>
              )}
              <p className="overview-total">
                {formatMoney(budgetedExpenses, settings, 0)} spent of{" "}
                {formatMoney(allocated, settings, 0)}
              </p>
            </Card>
          </section>
        </>
      )}
    </div>
  );
}
