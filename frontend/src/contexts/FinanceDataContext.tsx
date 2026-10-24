import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
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

  const refresh = useCallback(async () => {
    const [
      accounts,
      budgets,
      categories,
      goals,
      transactions,
      settings,
      transfers,
    ] = await Promise.allSettled([
      listAccounts(),
      listBudgets(),
      listCategories(),
      listGoals(),
      listTransactions(),
      getSettings(),
      listTransfers(),
    ] as const);

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
    const message = "This data could not be loaded. Please try again.";
    setErrors({
      accounts: accounts.status === "rejected" ? message : null,
      budgets: budgets.status === "rejected" ? message : null,
      categories: categories.status === "rejected" ? message : null,
      goals: goals.status === "rejected" ? message : null,
      transactions: transactions.status === "rejected" ? message : null,
      settings: settings.status === "rejected" ? message : null,
      transfers: transfers.status === "rejected" ? message : null,
    });
    setLoading(false);
  }, []);

  useEffect(() => {
    // Initial server-state hydration is intentionally triggered on mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refresh();
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
