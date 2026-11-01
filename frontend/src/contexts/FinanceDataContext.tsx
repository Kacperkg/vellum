import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { listAccounts } from "@/api/endpoints/accounts";
import { listBudgets } from "@/api/endpoints/budgets";
import { listCategories } from "@/api/endpoints/categories";
import { listGoals } from "@/api/endpoints/goals";
import { getSettings } from "@/api/endpoints/settings";
import { listTransactions } from "@/api/endpoints/transactions";
import { listTransfers } from "@/api/endpoints/transfers";
import { getStoredUser } from "@/api/tokenStorage";
import type {
  Account,
  Budget,
  Category,
  Goal,
  Transaction,
  Transfer,
  User,
  UserSettings,
} from "@/api/types";

import { apiErrorMessage } from "@/api/errors";
import { validateMoneyRows } from "@/utils/finance";

type FinanceData = {
  accounts: Account[];
  budgets: Budget[];
  categories: Category[];
  goals: Goal[];
  transactions: Transaction[];
  transfers: Transfer[];
  settings: UserSettings | null;
};

type FinanceDataContextValue = FinanceData & {
  user: User | null;
  loading: boolean;
  errors: Record<keyof FinanceData, string | null>;
  refresh: () => Promise<void>;
};

const emptyData: FinanceData = {
  accounts: [],
  budgets: [],
  categories: [],
  goals: [],
  transactions: [],
  transfers: [],
  settings: null,
};

const emptyErrors: Record<keyof FinanceData, string | null> = {
  accounts: null,
  budgets: null,
  categories: null,
  goals: null,
  transactions: null,
  transfers: null,
  settings: null,
};

const FinanceDataContext = createContext<FinanceDataContextValue | null>(null);

export function FinanceDataProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<FinanceData>(emptyData);
  const [loading, setLoading] = useState(true);
  const [errors, setErrors] = useState(emptyErrors);
  const refreshState = useRef({ version: 0 });

  const refresh = useCallback(async () => {
    const version = ++refreshState.current.version;
    const [
      accounts,
      budgets,
      categories,
      goals,
      transactions,
      settings,
      transfers,
    ] = await Promise.allSettled([
      listAccounts().then((rows) =>
        validateMoneyRows(rows, [
          "balance",
          "allocated_amount",
          "unallocated_amount",
        ]),
      ),
      listBudgets().then((rows) =>
        validateMoneyRows(rows, ["allocated_amount"]),
      ),
      listCategories(),
      listGoals().then((rows) =>
        validateMoneyRows(rows, [
          "target_amount",
          "saved_amount",
          "monthly_contribution",
        ]),
      ),
      listTransactions().then((rows) => validateMoneyRows(rows, ["amount"])),
      getSettings(),
      listTransfers().then((rows) => validateMoneyRows(rows, ["amount"])),
    ] as const);

    if (version !== refreshState.current.version) return;
    setData((current) => ({
      accounts:
        accounts.status === "fulfilled" ? accounts.value : current.accounts,
      budgets: budgets.status === "fulfilled" ? budgets.value : current.budgets,
      categories:
        categories.status === "fulfilled"
          ? categories.value
          : current.categories,
      goals: goals.status === "fulfilled" ? goals.value : current.goals,
      transactions:
        transactions.status === "fulfilled"
          ? transactions.value
          : current.transactions,
      settings:
        settings.status === "fulfilled" ? settings.value : current.settings,
      transfers:
        transfers.status === "fulfilled" ? transfers.value : current.transfers,
    }));
    const loadError = (
      result: PromiseSettledResult<unknown>,
    ): string | null => {
      if (result.status === "fulfilled") return null;
      const message = apiErrorMessage(
        result.reason,
        "This data could not be loaded. Please try again.",
      );
      if (
        result.reason instanceof RangeError ||
        /monetary|supported.*range|safe.*range|money must remain within/i.test(
          message,
        )
      ) {
        return "This financial data exceeds the supported exact monetary range (9007199254740991 minor units). Existing records have not been changed; review them before retrying.";
      }
      return "This data could not be loaded. Please try again.";
    };
    setErrors({
      accounts: loadError(accounts),
      budgets: loadError(budgets),
      categories: loadError(categories),
      goals: loadError(goals),
      transactions: loadError(transactions),
      settings: loadError(settings),
      transfers: loadError(transfers),
    });
    setLoading(false);
  }, []);

  useEffect(() => {
    // Initial server-state hydration is intentionally triggered on mount.
    const state = refreshState.current;
    void refresh();
    // Reconcile accounts created/deleted in another tab when returning here.
    const onFocus = () => void refresh();
    window.addEventListener("focus", onFocus);
    return () => {
      window.removeEventListener("focus", onFocus);
      state.version++;
    };
  }, [refresh]);

  const value = useMemo(
    () => ({
      ...data,
      user: getStoredUser(),
      loading,
      errors,
      refresh,
    }),
    [data, errors, loading, refresh],
  );

  return (
    <FinanceDataContext.Provider value={value}>
      {children}
    </FinanceDataContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useFinanceData(): FinanceDataContextValue {
  const value = useContext(FinanceDataContext);
  if (!value) {
    throw new Error("useFinanceData must be used inside FinanceDataProvider");
  }
  return value;
}
