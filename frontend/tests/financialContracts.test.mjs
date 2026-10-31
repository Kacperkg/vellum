import { after, test } from "node:test";
import assert from "node:assert/strict";
import { rm } from "node:fs/promises";
import { join } from "node:path";
import { createRequire } from "node:module";
import { compileModules } from "./helpers/compileModules.mjs";

const directory = await compileModules({
  finance: new URL("../src/utils/finance.ts", import.meta.url),
  activity: new URL(
    "../src/features/transactions/utils/activity.ts",
    import.meta.url,
  ),
  transactionExport: new URL(
    "../src/features/transactions/utils/transactionExport.ts",
    import.meta.url,
  ),
});
after(() => rm(directory, { recursive: true, force: true }));
const require = createRequire(import.meta.url);
const {
  majorToMinor,
  minorToMajor,
  formatMoney,
  moneyInteger,
  netAmount,
  calendarDate,
  formatDate,
  monthKey,
  isCurrentMonth,
  validateMoneyRows,
} = require(join(directory, "finance.js"));
const {
  activityNetAmount,
  financialActivity,
  recordedAccountBalance,
  sortFinancialActivity,
} = require(join(directory, "activity.js"));
const settings = { currency: "GBP", number_format: "comma_decimal" };
const maximum = Number.MAX_SAFE_INTEGER;
const base = {
  id: "one",
  name: "Boundary",
  amount: maximum,
  account_id: "one",
  category_id: "income",
  date: "2026-10-31T12:00:00Z",
  frequency: "one-off",
  type: "income",
};

test("decimal input preserves every penny at both supported boundaries", () => {
  assert.equal(majorToMinor("0.01"), 1);
  assert.equal(majorToMinor(".01"), 1);
  assert.equal(majorToMinor("90071992547409.91"), maximum);
  assert.equal(majorToMinor("90071992547409.90"), maximum - 1);
  assert.equal(majorToMinor("1.10"), 110);
  assert.equal(majorToMinor("0", true), 0);
  for (const value of [
    "0",
    "-0.01",
    "90071992547409.92",
    "9007199254740993",
    "0.001",
    "1.005",
    "NaN",
    "Infinity",
    "1e3",
    "",
    " 1.00",
  ]) {
    assert.throws(() => majorToMinor(value), RangeError, value);
  }
});

test("formatting and edit/CSV text preserve the final penny without floating rounding", () => {
  assert.equal(formatMoney(maximum, settings), "£90,071,992,547,409.91");
  assert.equal(formatMoney(maximum - 1, settings), "£90,071,992,547,409.90");
  assert.equal(minorToMajor(maximum), "90071992547409.91");
  assert.equal(majorToMinor(minorToMajor(maximum)), maximum);
  assert.equal(minorToMajor(-maximum), "-90071992547409.91");
  assert.equal(formatMoney(-1, settings), "-£0.01");
  assert.equal(formatMoney(149, settings, 0), "£1");
  assert.equal(formatMoney(150, settings, 0), "£2");
  assert.equal(formatMoney(-150, settings, 0), "-£2");
  for (const currency of ["GBP", "USD", "EUR"])
    for (const number_format of [
      "comma_decimal",
      "dot_decimal",
      "apostrophe",
    ]) {
      const display = { currency, number_format };
      // For ordinary amounts retain the established currency and grouping styles.
      const locale = {
        comma_decimal: "en-US",
        dot_decimal: "de-DE",
        apostrophe: "de-CH",
      }[number_format];
      assert.equal(
        formatMoney(123456, display),
        new Intl.NumberFormat(locale, {
          style: "currency",
          currency,
          minimumFractionDigits: 2,
          maximumFractionDigits: 2,
        }).format(1234.56),
      );
    }
});

test("derived net and account histories stay exact beyond wire and Go integer bounds", () => {
  const rows = [
    base,
    { ...base, id: "two" },
    { ...base, id: "three", type: "expense", amount: 1 },
  ];
  assert.equal(netAmount(rows), 18014398509481981n);
  assert.equal(
    activityNetAmount(financialActivity(rows, [])),
    18014398509481981n,
  );
  assert.equal(
    formatMoney(netAmount(rows), settings),
    "£180,143,985,094,819.81",
  );
  const many = Array.from({ length: 2048 }, (_, i) => ({
    ...base,
    id: String(i),
  }));
  assert.equal(recordedAccountBalance("one", many, []), 18446744073709549568n);
  assert.equal(
    formatMoney(netAmount(many), settings),
    "£184,467,440,737,095,495.68",
  );
  const cancelling = [
    ...many,
    ...many.map((row) => ({ ...row, type: "expense" })),
    { ...base, amount: 1 },
  ];
  assert.equal(netAmount(cancelling), 1n);
});

test("transfers conserve exact minor units and invalid JSON money fails closed", () => {
  const transfer = {
    id: "transfer",
    name: "Boundary transfer",
    amount: maximum,
    from_account_id: "one",
    to_account_id: "two",
    date: base.date,
    frequency: "one-off",
  };
  assert.equal(activityNetAmount(financialActivity([], [transfer])), 0n);
  assert.equal(
    recordedAccountBalance("one", [], [transfer]),
    -9007199254740991n,
  );
  assert.equal(
    recordedAccountBalance("two", [], [transfer]),
    9007199254740991n,
  );
  for (const amount of [
    maximum + 1,
    JSON.parse("9007199254740993"),
    1.5,
    Infinity,
    NaN,
  ]) {
    assert.throws(() => moneyInteger(amount), RangeError);
    assert.throws(
      () => validateMoneyRows([{ amount }], ["amount"]),
      RangeError,
    );
    assert.throws(() => formatMoney(amount, settings), RangeError);
  }
  assert.deepEqual(validateMoneyRows([{ amount: maximum }], ["amount"]), [
    { amount: maximum },
  ]);
});

test("calendar display and monthly grouping preserve written dates in every extreme timezone", () => {
  const before = process.env.TZ;
  try {
    for (const timezone of [
      "Etc/GMT+12",
      "Pacific/Kiritimati",
      "Europe/London",
      "UTC",
    ]) {
      process.env.TZ = timezone;
      for (const value of [
        "2026-10-31T12:00:00Z",
        "2026-10-31T23:59:59-12:00",
        "2026-10-31T00:00:00+14:00",
        "2026-10-31",
      ]) {
        assert.equal(calendarDate(value), "2026-10-31", timezone);
        assert.equal(formatDate(value, "YYYY-MM-DD"), "2026-10-31", timezone);
        assert.equal(formatDate(value, "DD/MM/YYYY"), "31/10/2026", timezone);
        assert.equal(formatDate(value, "MM/DD/YYYY"), "10/31/2026", timezone);
        assert.equal(monthKey(value), "2026-10", timezone);
        assert.equal(
          isCurrentMonth(value, new Date(2026, 9, 15, 12)),
          true,
          timezone,
        );
      }
    }
  } finally {
    if (before === undefined) delete process.env.TZ;
    else process.env.TZ = before;
  }
});

test("date-only, leap-day, DST and year/month boundaries share the same financial calendar", () => {
  for (const [value, day] of [
    ["2024-02-29", "2024-02-29"],
    ["2026-12-31T23:59:59-12:00", "2026-12-31"],
    ["2027-01-01T00:00:00+14:00", "2027-01-01"],
    ["2026-03-29T00:30:00Z", "2026-03-29"],
    ["2026-03-29T02:30:00+01:00", "2026-03-29"],
    ["2026-10-25T01:30:00+01:00", "2026-10-25"],
    ["2026-10-25T01:30:00Z", "2026-10-25"],
    ["2026-03-01T00:00:00Z", "2026-03-01"],
  ]) {
    assert.equal(calendarDate(value), day);
    assert.equal(monthKey(value), day.slice(0, 7));
  }
  for (const value of [
    "2026-10-31junk",
    "2026-10-31T24:00:00Z",
    "2026-10-31T12:60:00Z",
    "2026-10-31T12:00:60Z",
    "2026-10-31T12:00:00+24:00",
    "2026-10-31T12:00:00+01:60",
    "0001-01-01",
    "2026-02-29",
    "2024-02-30",
    "2026-13-01",
    "0000-01-01",
    "10/31/2026",
    "",
  ])
    assert.throws(() => calendarDate(value), RangeError, value);
  const early = { ...base, id: "early", date: "2026-10-31T23:59:00-12:00" };
  const later = { ...base, id: "later", date: "2026-11-01T00:01:00+14:00" };
  assert.deepEqual(
    sortFinancialActivity(
      [later, early],
      "date",
      "asc",
      new Map(),
      new Map(),
    ).map((row) => row.id),
    ["early", "later"],
  );
});

test("CSV exports calendar days and exact boundary cents", async (t) => {
  const { exportTransactions } = require(
    join(directory, "transactionExport.js"),
  );
  const blobs = [];
  t.mock.method(URL, "createObjectURL", (blob) => {
    blobs.push(blob);
    return "blob:test";
  });
  t.mock.method(URL, "revokeObjectURL", () => {});
  const old = globalThis.document;
  globalThis.document = {
    body: { appendChild() {} },
    createElement: () => ({ click() {}, remove() {} }),
  };
  t.after(() => {
    if (old) globalThis.document = old;
    else delete globalThis.document;
  });
  exportTransactions([base], [], [], settings);
  const text = await blobs[0].text();
  assert.match(text, /"2026-10-31"/);
  assert.match(text, /,90071992547409\.91,90071992547409\.91,/);
});

test("legacy UTC noon and midnight records keep their displayed financial month", () => {
  const original = process.env.TZ;
  try {
    process.env.TZ = "Pacific/Kiritimati";
    assert.equal(monthKey("2026-10-31T12:00:00Z"), "2026-10");
    assert.equal(
      formatDate("2026-10-31T12:00:00Z", "YYYY-MM-DD"),
      "2026-10-31",
    );
    process.env.TZ = "Etc/GMT+12";
    assert.equal(monthKey("2026-03-01T00:00:00Z"), "2026-03");
    assert.equal(
      formatDate("2026-03-01T00:00:00Z", "DD/MM/YYYY"),
      "01/03/2026",
    );
  } finally {
    if (original === undefined) delete process.env.TZ;
    else process.env.TZ = original;
  }
});
