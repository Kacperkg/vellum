import { useMemo, useState } from "react";
import Button from "../components/Button";
import Card from "../components/Card";
import DataState from "../components/DataState";
import Icon from "../components/Icon";
import PageHeader from "../components/PageHeader";
import NewTransactionDialog from "../components/dialog/NewTransactionDialog";
import NewTransferDialog from "../components/dialog/NewTransferDialog";
import EditTransactionDialog from "../components/dialog/EditTransactionDialog";
import DeleteDialog from "../components/dialog/DeleteDialog";
import Select from "../components/Select";
import Input from "../components/Input";
import { deleteTransfer } from "../api/endpoints/transfers";
import {
  activityNetAmount,
  financialActivity,
  formatActivityAmount,
  sortFinancialActivity,
  type ActivitySortKey,
  type ActivitySortDirection,
  type FinancialActivity,
} from "../lib/activity";
import { deleteTransaction } from "../api/endpoints/transactions";
import { exportTransactions } from "../lib/transactionExport";
import { useFinanceData } from "../context/FinanceDataContext";
import { formatDate, formatMoney } from "../lib/finance";

const sortColumns: { key: ActivitySortKey; label: string }[] = [
  { key: "merchant", label: "Merchant" },
  { key: "category", label: "Category" },
  { key: "account", label: "Account" },
  { key: "date", label: "Date" },
  { key: "amount", label: "Amount" },
];

export default function Transactions() {
  const {
    accounts,
    categories,
    transactions,
    transfers,
    settings,
    loading,
    errors,
    refresh,
  } = useFinanceData();
  const error =
    errors.accounts ??
    errors.categories ??
    errors.transactions ??
    errors.transfers ??
    errors.settings;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [categoryId, setCategoryId] = useState("all");
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<FinancialActivity | null>(null);
  const [deleting, setDeleting] = useState<FinancialActivity | null>(null);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [accountId, setAccountId] = useState("all");
  const [transactionType, setTransactionType] = useState("all");
  const [frequency, setFrequency] = useState("all");
  const [startDate, setStartDate] = useState("");
  const [endDate, setEndDate] = useState("");
  const [exportStatus, setExportStatus] = useState("");
  const [editMode, setEditMode] = useState(false);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<ActivitySortKey>("date");
  const [sortDirection, setSortDirection] =
    useState<ActivitySortDirection>("desc");

  function changeSort(key: ActivitySortKey) {
    setSortKey(key);
    setSortDirection(
      key === sortKey
        ? sortDirection === "asc"
          ? "desc"
          : "asc"
        : key === "date" || key === "amount"
          ? "desc"
          : "asc",
    );
  }

  function clearFilters() {
    setAccountId("all");
    setTransactionType("all");
    setFrequency("all");
    setStartDate("");
    setEndDate("");
    setCategoryId("all");
    setQuery("");
  }

  const categoryById = useMemo(
    () => new Map(categories.map((item) => [item.id, item.name])),
    [categories],
  );
  const accountById = useMemo(
    () => new Map(accounts.map((item) => [item.id, item.name])),
    [accounts],
  );
  const visible = useMemo(
    () =>
      sortFinancialActivity(
        financialActivity(transactions, transfers).filter((item) => {
          const category =
            item.type === "transfer"
              ? "Transfer"
              : (categoryById.get(item.category_id) ?? "Uncategorised");
          return (
            (categoryId === "all" ||
              (categoryId === "transfers"
                ? item.type === "transfer"
                : item.category_id === categoryId)) &&
            (accountId === "all" ||
              item.account_id === accountId ||
              (item.type === "transfer" && item.to_account_id === accountId)) &&
            (transactionType === "all" || item.type === transactionType) &&
            (frequency === "all" || item.frequency === frequency) &&
            (!startDate || item.date.slice(0, 10) >= startDate) &&
            (!endDate || item.date.slice(0, 10) <= endDate) &&
            `${item.name} ${category} ${accountById.get(item.account_id) ?? ""} ${item.type === "transfer" ? (accountById.get(item.to_account_id) ?? "") : ""}`
              .toLowerCase()
              .includes(query.toLowerCase())
          );
        }),
        sortKey,
        sortDirection,
        accountById,
        categoryById,
        accountId,
      ),
    [
      categoryById,
      categoryId,
      query,
      transactions,
      transfers,
      accountById,
      accountId,
      transactionType,
      frequency,
      startDate,
      endDate,
      sortKey,
      sortDirection,
    ],
  );
  const selected = visible.find(
    (item) => `${item.type}:${item.id}` === selectedKey,
  );

  return (
    <div className="page-shell transactions-page">
      <PageHeader
        eyebrow="Activity"
        title="Transactions"
        description={
          settings
            ? `${visible.length} ${visible.length === 1 ? "entry" : "entries"} · net ${formatMoney(activityNetAmount(visible, accountId), settings)}`
            : "Your recorded activity"
        }
        actions={
          <>
            <Button
              variant="outline"
              disabled={loading || !!error || !settings || visible.length === 0}
              onClick={() => {
                if (!settings) return;
                exportTransactions(
                  visible,
                  accounts,
                  categories,
                  settings,
                  accountId,
                );
                setExportStatus(
                  `Exported ${visible.length} ${visible.length === 1 ? "transaction" : "transactions"}.`,
                );
              }}
            >
              <Icon name="download" />
              Export
            </Button>
            <Button variant="outline" onClick={() => setTransferOpen(true)}>
              <Icon name="transactions" />
              Transfer
            </Button>
            <Button onClick={() => setDialogOpen(true)}>
              <Icon name="plus" />
              Add transaction
            </Button>
          </>
        }
      />
      {dialogOpen && (
        <NewTransactionDialog
          dialogOpen={dialogOpen}
          setDialogOpen={setDialogOpen}
        />
      )}
      {transferOpen && (
        <NewTransferDialog
          dialogOpen={transferOpen}
          setDialogOpen={setTransferOpen}
        />
      )}
      {editing?.type === "transfer" && (
        <NewTransferDialog
          key={editing.id}
          transfer={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {editing && editing.type !== "transfer" && (
        <EditTransactionDialog
          transaction={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          key={deleting.id}
          type={deleting.type === "transfer" ? "Transfer" : "Transaction"}
          input={deleting.name}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={async () => {
            if (deleting.type === "transfer") await deleteTransfer(deleting.id);
            else await deleteTransaction(deleting.id);
            setSelectedKey(null);
            await refresh();
          }}
        />
      )}
      {exportStatus && (
        <p role="status" className="action-status">
          {exportStatus}
        </p>
      )}
      <DataState
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
      />
      {!loading && !error && settings && (
        <Card className="transactions-card">
          <div className="transaction-tools">
            <label>
              <Icon name="search" />
              <input
                aria-label="Search merchant, category or account"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search merchant, category or account..."
              />
            </label>
            <Button
              variant="outline"
              aria-expanded={filtersOpen}
              aria-controls="transaction-filters"
              onClick={() => setFiltersOpen((open) => !open)}
            >
              <Icon name="filter" />
              Filters
            </Button>
            <Button
              className="transaction-edit-toggle"
              variant={editMode ? "secondary" : "outline"}
              aria-label={
                editMode ? "Done editing transactions" : "Edit transactions"
              }
              aria-pressed={editMode}
              title={
                editMode ? "Done editing transactions" : "Edit transactions"
              }
              disabled={visible.length === 0 && !editMode}
              onClick={() => {
                setEditMode((active) => !active);
                setSelectedKey(null);
              }}
            >
              <Icon name="edit" />
            </Button>
          </div>
          {editMode && (
            <div className="transaction-edit-bar">
              <p role="status">
                {selected
                  ? selected.name
                  : "Select a transaction to edit or delete."}
              </p>
              <div>
                <Button
                  variant="outline"
                  disabled={!selected}
                  onClick={() => {
                    if (selected) setEditing(selected);
                  }}
                >
                  <Icon name="edit" />
                  Edit selected
                </Button>
                <Button
                  variant="destructive"
                  disabled={!selected}
                  onClick={() => {
                    if (selected) setDeleting(selected);
                  }}
                >
                  <Icon name="trash" />
                  Delete selected
                </Button>
              </div>
            </div>
          )}
          {filtersOpen && (
            <div id="transaction-filters" className="transaction-filters">
              <label>
                Account
                <Select
                  value={accountId}
                  onChange={(event) => setAccountId(event.target.value)}
                >
                  <option value="all">All accounts</option>
                  {accounts.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
                </Select>
              </label>
              <label>
                Type
                <Select
                  value={transactionType}
                  onChange={(event) => setTransactionType(event.target.value)}
                >
                  <option value="all">All types</option>
                  <option value="expense">Expense</option>
                  <option value="income">Income</option>
                  <option value="transfer">Transfer</option>
                </Select>
              </label>
              <label>
                Frequency
                <Select
                  value={frequency}
                  onChange={(event) => setFrequency(event.target.value)}
                >
                  <option value="all">All frequencies</option>
                  <option value="one-off">One-off</option>
                  <option value="daily">Daily</option>
                  <option value="weekly">Weekly</option>
                  <option value="monthly">Monthly</option>
                  <option value="yearly">Yearly</option>
                </Select>
              </label>
              <label>
                From
                <Input
                  variant="date"
                  value={startDate}
                  max={endDate || undefined}
                  onChange={(event) => setStartDate(event.target.value)}
                />
              </label>
              <label>
                To
                <Input
                  variant="date"
                  value={endDate}
                  min={startDate || undefined}
                  onChange={(event) => setEndDate(event.target.value)}
                />
              </label>
              <Button variant="ghost" onClick={clearFilters}>
                Clear filters
              </Button>
            </div>
          )}
          <div className="category-tabs" aria-label="Transaction categories">
            <button
              className={categoryId === "all" ? "active" : ""}
              aria-pressed={categoryId === "all"}
              onClick={() => setCategoryId("all")}
            >
              All
            </button>
            <button
              className={categoryId === "transfers" ? "active" : ""}
              aria-pressed={categoryId === "transfers"}
              onClick={() => setCategoryId("transfers")}
            >
              Transfers
            </button>
            {categories.map((item) => (
              <button
                key={item.id}
                className={categoryId === item.id ? "active" : ""}
                aria-pressed={categoryId === item.id}
                onClick={() => setCategoryId(item.id)}
              >
                {item.name}
              </button>
            ))}
          </div>
          <div className="transaction-sort-controls">
            <label htmlFor="transaction-sort">Sort by</label>
            <Select
              id="transaction-sort"
              value={sortKey}
              onChange={(event) =>
                setSortKey(event.target.value as ActivitySortKey)
              }
            >
              {sortColumns.map((column) => (
                <option key={column.key} value={column.key}>
                  {column.label}
                </option>
              ))}
            </Select>
            <Button
              variant="outline"
              aria-label={
                sortDirection === "asc" ? "Sort descending" : "Sort ascending"
              }
              title={
                sortDirection === "asc" ? "Sort descending" : "Sort ascending"
              }
              onClick={() =>
                setSortDirection((direction) =>
                  direction === "asc" ? "desc" : "asc",
                )
              }
            >
              <span aria-hidden="true">
                {sortDirection === "asc" ? "↑" : "↓"}
              </span>
            </Button>
          </div>
          {visible.length === 0 ? (
            <DataState
              loading={false}
              error={null}
              empty
              emptyTitle={
                transactions.length === 0 && transfers.length === 0
                  ? "No transactions yet"
                  : "No matching transactions"
              }
              emptyBody={
                transactions.length === 0 && transfers.length === 0
                  ? "Add a transaction to start building your live activity history."
                  : "Adjust your search or clear filters to see more activity."
              }
            />
          ) : (
            <div
              className="transaction-table-wrap"
              role="table"
              aria-label="Transaction activity"
            >
              <div className="transaction-row transaction-head" role="row">
                {sortColumns.map((column) => (
                  <span
                    key={column.key}
                    role="columnheader"
                    aria-sort={
                      sortKey === column.key
                        ? sortDirection === "asc"
                          ? "ascending"
                          : "descending"
                        : "none"
                    }
                  >
                    <button
                      onClick={() => changeSort(column.key)}
                      aria-label={`Sort by ${column.label.toLowerCase()}, ${sortKey === column.key && sortDirection === "asc" ? "descending" : sortKey === column.key && sortDirection === "desc" ? "ascending" : column.key === "date" || column.key === "amount" ? "descending" : "ascending"}`}
                    >
                      {column.label}
                      <span aria-hidden="true">
                        {sortKey === column.key
                          ? sortDirection === "asc"
                            ? "↑"
                            : "↓"
                          : "↕"}
                      </span>
                    </button>
                  </span>
                ))}
              </div>
              {visible.map((item) => (
                <div
                  className={`transaction-row ${editMode && selectedKey === `${item.type}:${item.id}` ? "transaction-selected" : ""}`}
                  key={`${item.type}:${item.id}`}
                  role="row"
                >
                  <div className="merchant-cell" role="cell">
                    {editMode && (
                      <label className="transaction-select">
                        <input
                          type="radio"
                          name="selected-transaction"
                          aria-label={`Select ${item.name}`}
                          checked={selectedKey === `${item.type}:${item.id}`}
                          onChange={() =>
                            setSelectedKey(`${item.type}:${item.id}`)
                          }
                        />
                      </label>
                    )}
                    <span className="merchant-mark">
                      {item.name[0]?.toUpperCase()}
                    </span>
                    <div>
                      <strong>{item.name}</strong>
                    </div>
                  </div>
                  <span data-label="Category" role="cell">
                    {item.type === "transfer"
                      ? "Transfer"
                      : (categoryById.get(item.category_id) ?? "Uncategorised")}
                  </span>
                  <span data-label="Account" role="cell">
                    {accountById.get(item.account_id) ?? "Unknown account"}
                    {item.type === "transfer" && (
                      <>
                        {" "}
                        →{" "}
                        {accountById.get(item.to_account_id) ??
                          "Unknown account"}
                      </>
                    )}
                  </span>
                  <span data-label="Date" role="cell">
                    {formatDate(item.date, settings.date_format)}
                  </span>
                  <strong
                    role="cell"
                    className={item.type === "income" ? "positive" : ""}
                  >
                    {formatActivityAmount(item, settings, accountId)}
                  </strong>
                </div>
              ))}
            </div>
          )}
        </Card>
      )}
    </div>
  );
}
