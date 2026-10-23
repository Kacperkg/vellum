import { useEffect, useId, useMemo, useState } from "react";
import Card from "../components/Card";
import DataState from "../components/DataState";
import PageHeader from "../components/PageHeader";
import { useFinanceData } from "../context/FinanceDataContext";
import { formatMoney, monthKey } from "../lib/finance";

function ChartHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="chart-header">
      <h2>{title}</h2>
      <p>{subtitle}</p>
    </div>
  );
}

export default function Analytics() {
  const [activeMonthKey, setActiveMonthKey] = useState<string | null>(null);
  const tooltipId = useId();
  useEffect(() => {
    if (!activeMonthKey) return;
    const dismiss = (event: KeyboardEvent) => {
      if (event.key === "Escape") setActiveMonthKey(null);
    };
    document.addEventListener("keydown", dismiss);
    return () => document.removeEventListener("keydown", dismiss);
  }, [activeMonthKey]);
  const { categories, transactions, settings, loading, errors, refresh } =
    useFinanceData();
  const error = errors.categories ?? errors.transactions ?? errors.settings;
  const analytics = useMemo(() => {
    const now = new Date();
    const months = Array.from({ length: 6 }, (_, index) => {
      const date = new Date(now.getFullYear(), now.getMonth() - (5 - index), 1);
      return {
        key: monthKey(date),
        label: new Intl.DateTimeFormat("en", { month: "short" }).format(date),
        fullLabel: new Intl.DateTimeFormat("en", {
          month: "long",
          year: "numeric",
        }).format(date),
        income: 0,
        expenses: 0,
      };
    });
    const monthByKey = new Map(months.map((item) => [item.key, item]));
    transactions.forEach((transaction) => {
      const month = monthByKey.get(monthKey(transaction.date));
      if (!month) return;
      if (transaction.type === "income") month.income += transaction.amount;
      else month.expenses += transaction.amount;
    });
    const categoryTotals = new Map<string, number>();
    transactions
      .filter(
        (item) =>
          item.type === "expense" && monthByKey.has(monthKey(item.date)),
      )
      .forEach((item) =>
        categoryTotals.set(
          item.category_id,
          (categoryTotals.get(item.category_id) ?? 0) + item.amount,
        ),
      );
    const topCategories = [...categoryTotals.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 6);
    return { months, topCategories };
  }, [transactions]);

  const maxMonthly = Math.max(
    ...analytics.months.flatMap((month) => [month.income, month.expenses]),
    1,
  );
  const activeMonth = analytics.months.find(
    (month) => month.key === activeMonthKey,
  );
  const axisDigits = maxMonthly < 10000 ? 2 : 0;
  const categoryTotal = analytics.topCategories.reduce(
    (sum, [, value]) => sum + value,
    0,
  );
  const colors = [
    "#e2416c",
    "#87cfa0",
    "#70a6d8",
    "#e7bd5c",
    "#8b72c7",
    "#85899b",
  ];
  const categorySegments = analytics.topCategories.map(
    ([id, amount], index) => ({
      id,
      name:
        categories.find((category) => category.id === id)?.name ??
        "Uncategorised",
      amount,
      color: colors[index],
      percentage: categoryTotal > 0 ? (amount / categoryTotal) * 100 : 0,
    }),
  );
  const donut = categorySegments
    .reduce<{ position: number; segments: string[] }>(
      (result, category) => {
        const end = result.position + category.percentage;
        return {
          position: end,
          segments: [
            ...result.segments,
            `${category.color} ${result.position}% ${end}%`,
          ],
        };
      },
      { position: 0, segments: [] },
    )
    .segments.join(", ");

  return (
    <div className="page-shell analytics-page">
      <PageHeader
        eyebrow="Last 6 months"
        title="Analytics"
        description="A quiet, considered view of how your money moves."
      />
      <DataState
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
      />
      {!loading &&
        !error &&
        settings &&
        (transactions.length === 0 ? (
          <DataState
            loading={false}
            error={null}
            empty
            emptyTitle="No analytics yet"
            emptyBody="Your live charts will appear after you record transactions."
          />
        ) : (
          <section className="analytics-grid">
            <Card className="chart-card income-chart-card">
              <ChartHeader
                title="Income vs Expenses"
                subtitle="Monthly comparison"
              />
              <div
                className="bar-chart"
                role="group"
                aria-label="Live income and expenses comparison for the last six months"
              >
                <div className="y-labels">
                  <span>{formatMoney(maxMonthly, settings, axisDigits)}</span>
                  <span>
                    {formatMoney(maxMonthly * 0.75, settings, axisDigits)}
                  </span>
                  <span>
                    {formatMoney(maxMonthly * 0.5, settings, axisDigits)}
                  </span>
                  <span>
                    {formatMoney(maxMonthly * 0.25, settings, axisDigits)}
                  </span>
                  <span>{formatMoney(0, settings, axisDigits)}</span>
                </div>
                <div className="bar-plot">
                  {[0, 1, 2, 3, 4].map((line) => (
                    <i key={line} style={{ top: `${line * 25}%` }} />
                  ))}
                  {analytics.months.map((month) => (
                    <button
                      className={`bar-group ${activeMonthKey === month.key ? "bar-group-active" : ""}`}
                      key={month.key}
                      aria-label={`${month.fullLabel}. Income ${formatMoney(month.income, settings)}. Expenses ${formatMoney(month.expenses, settings)}.`}
                      aria-describedby={
                        activeMonthKey === month.key ? tooltipId : undefined
                      }
                      onPointerEnter={() => setActiveMonthKey(month.key)}
                      onPointerLeave={(event) => {
                        if (!event.currentTarget.matches(":focus"))
                          setActiveMonthKey(null);
                      }}
                      onFocus={() => setActiveMonthKey(month.key)}
                      onBlur={() => setActiveMonthKey(null)}
                      onClick={() => setActiveMonthKey(month.key)}
                      onKeyDown={(event) => {
                        if (event.key === "Escape") setActiveMonthKey(null);
                      }}
                    >
                      <span className="bar-pair" aria-hidden="true">
                        <span
                          className="income-bar"
                          style={{
                            height: `${(month.income / maxMonthly) * 100}%`,
                          }}
                        />
                        <span
                          className="expense-bar"
                          style={{
                            height: `${(month.expenses / maxMonthly) * 100}%`,
                          }}
                        />
                      </span>
                      <span className="bar-month" aria-hidden="true">
                        {month.label}
                      </span>
                    </button>
                  ))}
                  {activeMonth && (
                    <div id={tooltipId} className="bar-tooltip" role="tooltip">
                      <strong>{activeMonth.fullLabel}</strong>
                      <span>
                        <i className="income-bar" />
                        Income{" "}
                        <b>{formatMoney(activeMonth.income, settings)}</b>
                      </span>
                      <span>
                        <i className="expense-bar" />
                        Expenses{" "}
                        <b>{formatMoney(activeMonth.expenses, settings)}</b>
                      </span>
                    </div>
                  )}
                </div>
              </div>
              <div className="bar-chart-legend" aria-label="Chart series">
                <span>
                  <i className="income-bar" />
                  Income
                </span>
                <span>
                  <i className="expense-bar" />
                  Expenses
                </span>
              </div>
            </Card>
            <Card className="chart-card category-chart-card">
              <ChartHeader
                title="Category split"
                subtitle={`${categorySegments.length} top ${categorySegments.length === 1 ? "category" : "categories"} · last 6 months`}
              />
              {categorySegments.length > 0 ? (
                <>
                  <div
                    className="donut-chart"
                    style={{
                      background: donut
                        ? `conic-gradient(${donut})`
                        : undefined,
                    }}
                    role="img"
                    aria-label={categorySegments
                      .map(
                        (category) =>
                          `${category.name}: ${formatMoney(category.amount, settings)}`,
                      )
                      .join(", ")}
                  />
                  <ul
                    className="category-legend"
                    aria-label="Category split legend"
                  >
                    {categorySegments.map((category) => (
                      <li key={category.id}>
                        <span
                          className="category-legend-swatch"
                          style={{ backgroundColor: category.color }}
                          aria-hidden="true"
                        />
                        <span className="category-legend-name">
                          {category.name}
                        </span>
                        <span className="category-legend-value">
                          <strong>
                            {formatMoney(category.amount, settings)}
                          </strong>
                          <small>
                            {Number(category.percentage.toFixed(1))}%
                          </small>
                        </span>
                      </li>
                    ))}
                  </ul>
                  <p className="category-legend-note">
                    Percentages show the share of these top categories.
                  </p>
                </>
              ) : (
                <DataState
                  loading={false}
                  error={null}
                  empty
                  emptyTitle="No category spending yet"
                  emptyBody="Expense categories from the last six months will appear here."
                />
              )}
            </Card>
          </section>
        ))}
    </div>
  );
}
