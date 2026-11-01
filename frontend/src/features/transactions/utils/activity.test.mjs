import { after, test } from "node:test";
import assert from "node:assert/strict";
import { rm } from "node:fs/promises";
import { join } from "node:path";
import { createRequire } from "node:module";
import { compileModules } from "../../../../tests/helpers/compileModules.mjs";

// Compile the production utilities with the project's existing TypeScript
// dependency. The normal build checks their types; these tests check accounting.
const buildDirectory = await compileModules({
  finance: new URL("../../../utils/finance.ts", import.meta.url),
  activity: new URL("./activity.ts", import.meta.url),
  transactionExport: new URL("./transactionExport.ts", import.meta.url),
});
after(() => rm(buildDirectory, { recursive: true, force: true }));
const require = createRequire(import.meta.url);
const {
  activityImpact,
  activityNetAmount,
  financialActivity,
  formatActivityAmount,
  recordedAccountBalance,
  sortFinancialActivity,
} = require(join(buildDirectory, "activity.js"));
const settings = { currency: "GBP", number_format: "comma_decimal" };
const payroll = {
  id: "pay",
  name: "Salary",
  amount: 100000,
  account_id: "payroll",
  category_id: "income",
  date: "2026-10-01T12:00:00Z",
  frequency: "one-off",
  type: "income",
};
const subscription = {
  id: "sub",
  name: "Streaming",
  amount: 1000,
  account_id: "bills",
  category_id: "subscriptions",
  date: "2026-10-03T12:00:00Z",
  frequency: "monthly",
  type: "expense",
};
const transfer = {
  id: "move",
  name: "Fund subscriptions",
  amount: 20000,
  from_account_id: "payroll",
  to_account_id: "bills",
  date: "2026-10-02T12:00:00Z",
  frequency: "one-off",
};
const transactions = [payroll, subscription];

test("salary, transfer and subscription affect the correct accounts without changing total net", () => {
  const activity = financialActivity(transactions, [transfer]);
  assert.equal(
    recordedAccountBalance("payroll", transactions, [transfer]),
    80000n,
  );
  assert.equal(
    recordedAccountBalance("bills", transactions, [transfer]),
    19000n,
  );
  assert.equal(activityNetAmount(activity), 99000n);
  assert.equal(activityNetAmount(activity, "payroll"), 80000n);
  assert.equal(activityNetAmount(activity, "bills"), 19000n);
  assert.equal(
    activityNetAmount(activity),
    recordedAccountBalance("payroll", transactions, [transfer]) +
      recordedAccountBalance("bills", transactions, [transfer]),
  );
});

test("editing the amount or destination replaces the entire transfer effect", () => {
  const edited = { ...transfer, amount: 35000, to_account_id: "savings" };
  assert.equal(
    recordedAccountBalance("payroll", transactions, [edited]),
    65000n,
  );
  assert.equal(recordedAccountBalance("bills", transactions, [edited]), -1000n);
  assert.equal(
    recordedAccountBalance("savings", transactions, [edited]),
    35000n,
  );
  assert.equal(
    activityNetAmount(financialActivity(transactions, [edited])),
    99000n,
  );
});

test("deleting a transfer reverses both legs and preserves income and expenses", () => {
  assert.equal(recordedAccountBalance("payroll", transactions, []), 100000n);
  assert.equal(recordedAccountBalance("bills", transactions, []), -1000n);
  assert.equal(activityNetAmount(financialActivity(transactions, [])), 99000n);
});

test("a return transfer cancels the original account movement", () => {
  const returned = {
    ...transfer,
    id: "return",
    from_account_id: "bills",
    to_account_id: "payroll",
  };
  assert.equal(
    recordedAccountBalance("payroll", transactions, [transfer, returned]),
    100000n,
  );
  assert.equal(
    recordedAccountBalance("bills", transactions, [transfer, returned]),
    -1000n,
  );
});

test("transfer display is neutral globally and directional when filtering either account", () => {
  const [item] = financialActivity([], [transfer]);
  assert.equal(item.type, "transfer");
  assert.equal(item.frequency, "one-off");
  assert.equal(item.category_id, null);
  assert.equal(activityImpact(item), 0);
  assert.equal(activityImpact(item, "unrelated"), 0);
  assert.equal(formatActivityAmount(item, settings), "↔ £200.00");
  assert.equal(formatActivityAmount(item, settings, "payroll"), "−£200.00");
  assert.equal(formatActivityAmount(item, settings, "bills"), "+£200.00");
});

test("history merges transfers by date without modifying the source arrays", () => {
  const activity = financialActivity(transactions, [transfer]);
  assert.deepEqual(
    activity.map((item) => item.id),
    ["sub", "move", "pay"],
  );
  assert.deepEqual(
    transactions.map((item) => item.id),
    ["pay", "sub"],
  );
});

test("recurring transfers retain their frequency without multiplying the recorded movement", () => {
  for (const frequency of ["daily", "weekly", "monthly", "yearly"]) {
    const recurring = { ...transfer, frequency };
    const activity = financialActivity(transactions, [recurring]);
    const moved = activity.find((item) => item.id === recurring.id);
    assert.equal(moved.type, "transfer");
    assert.equal(moved.frequency, frequency);
    assert.equal(
      activity.filter((item) => item.frequency !== "one-off").length,
      2,
    );
    assert.equal(
      recordedAccountBalance("payroll", transactions, [recurring]),
      80000n,
    );
    assert.equal(
      recordedAccountBalance("bills", transactions, [recurring]),
      19000n,
    );
    assert.equal(activityNetAmount(activity), 99000n);
  }
});

test("legacy transfers with no frequency retain one-off behavior", () => {
  const legacy = { ...transfer };
  delete legacy.frequency;
  assert.equal(financialActivity([], [legacy])[0].frequency, "one-off");
});

test("sorting follows displayed names, full dates and signed amounts without mutating activity", () => {
  const activity = financialActivity(transactions, [transfer]);
  const accounts = new Map([
    ["payroll", "Zebra"],
    ["bills", "Alpha"],
  ]);
  const categories = new Map([
    ["income", "Income"],
    ["subscriptions", "Subscriptions"],
  ]);
  const sort = (key, direction = "asc", accountId = "all") =>
    sortFinancialActivity(
      activity,
      key,
      direction,
      accounts,
      categories,
      accountId,
    ).map((item) => item.id);
  assert.deepEqual(sort("merchant"), ["move", "pay", "sub"]);
  assert.deepEqual(sort("category"), ["pay", "sub", "move"]);
  assert.deepEqual(sort("account"), ["sub", "pay", "move"]);
  assert.deepEqual(sort("date"), ["pay", "move", "sub"]);
  assert.deepEqual(sort("date", "desc"), ["sub", "move", "pay"]);
  assert.deepEqual(sort("amount"), ["sub", "move", "pay"]);
  assert.deepEqual(sort("amount", "desc"), ["pay", "move", "sub"]);
  assert.deepEqual(sort("amount", "asc", "payroll"), ["move", "sub", "pay"]);
  assert.deepEqual(
    activity.map((item) => item.id),
    ["sub", "move", "pay"],
  );
});

test("sorting resolves case-insensitive ties deterministically and treats ledger days as calendar dates", () => {
  const first = {
    ...payroll,
    id: "b",
    name: "item 2",
    date: "2026-10-24T08:00:00Z",
  };
  const second = {
    ...first,
    id: "a",
    name: "ITEM 2",
    date: "2026-10-24T16:00:00Z",
  };
  const third = { ...second, id: "c", name: "item 10" };
  const activity = financialActivity([third, first, second], []);
  const sort = (key) =>
    sortFinancialActivity(activity, key, "asc", new Map(), new Map()).map(
      (item) => item.id,
    );
  assert.deepEqual(sort("merchant"), ["a", "b", "c"]);
  assert.deepEqual(sort("date"), ["a", "b", "c"]);
});

test("CSV preserves both transfer accounts and distinguishes amount from net impact", async (t) => {
  const { exportTransactions } = require(
    join(buildDirectory, "transactionExport.js"),
  );
  const blobs = [];
  t.mock.method(URL, "createObjectURL", (blob) => {
    blobs.push(blob);
    return "blob:transfer-export-test";
  });
  const oldDocument = globalThis.document;
  globalThis.document = {
    body: { appendChild() {} },
    createElement: () => ({ click() {}, remove() {} }),
  };
  t.after(() => {
    if (oldDocument) globalThis.document = oldDocument;
    else delete globalThis.document;
  });
  const accounts = [
    { id: "payroll", name: "Payroll" },
    { id: "bills", name: "Bills" },
  ];
  const activity = financialActivity([], [transfer]);
  exportTransactions(activity, accounts, [], settings);
  exportTransactions(activity, accounts, [], settings, "bills");
  exportTransactions(
    financialActivity([], [{ ...transfer, frequency: "monthly" }]),
    accounts,
    [],
    settings,
  );
  const globalCsv = await blobs[0].text();
  const accountCsv = await blobs[1].text();
  assert.match(globalCsv, /"Payroll","Bills"/);
  assert.match(globalCsv, /"transfer",200\.00,0\.00,"GBP"/);
  assert.match(accountCsv, /"transfer",200\.00,200\.00,"GBP"/);
  assert.match(
    await blobs[2].text(),
    /"monthly","transfer",200\.00,0\.00,"GBP"/,
  );
});
