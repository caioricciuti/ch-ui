import { describe, it, expect } from "vitest";
import { escapeIdentifier, escapeStringLiteral, formatScopeSQL } from "../sqlEscape";

describe("escapeIdentifier", () => {
  it("wraps simple name in backticks", () => {
    expect(escapeIdentifier("username")).toBe("`username`");
  });

  it("escapes internal backticks", () => {
    expect(escapeIdentifier("user`name")).toBe("`user``name`");
  });

  it("handles names with hyphens", () => {
    expect(escapeIdentifier("my-database")).toBe("`my-database`");
  });

  it("handles names with dots", () => {
    expect(escapeIdentifier("my.database")).toBe("`my.database`");
  });

  it("handles names with spaces", () => {
    expect(escapeIdentifier("my database")).toBe("`my database`");
  });

  it("handles SQL injection attempt in identifier", () => {
    expect(escapeIdentifier("admin`; DROP TABLE users; --")).toBe(
      "`admin``; DROP TABLE users; --`"
    );
  });

  it("handles empty string", () => {
    expect(escapeIdentifier("")).toBe("``");
  });

  it("handles reserved keywords", () => {
    expect(escapeIdentifier("SELECT")).toBe("`SELECT`");
  });
});

describe("escapeStringLiteral", () => {
  it("returns simple string unchanged", () => {
    expect(escapeStringLiteral("password123")).toBe("password123");
  });

  it("escapes single quotes", () => {
    expect(escapeStringLiteral("pass'word")).toBe("pass\\'word");
  });

  it("escapes backslashes", () => {
    expect(escapeStringLiteral("pass\\word")).toBe("pass\\\\word");
  });

  it("escapes both backslash and single quote", () => {
    expect(escapeStringLiteral("pass\\'word")).toBe("pass\\\\\\'word");
  });

  it("handles SQL injection attempt in password", () => {
    expect(escapeStringLiteral("'; DROP TABLE users; --")).toBe(
      "\\'; DROP TABLE users; --"
    );
  });

  it("handles empty string", () => {
    expect(escapeStringLiteral("")).toBe("");
  });
});

describe("formatScopeSQL", () => {
  it("formats global scope", () => {
    expect(formatScopeSQL({ type: "global" })).toBe("*.*");
  });

  it("formats database scope", () => {
    expect(formatScopeSQL({ type: "database", database: "mydb" })).toBe("`mydb`.*");
  });

  it("formats table scope", () => {
    expect(
      formatScopeSQL({ type: "table", database: "mydb", table: "mytable" })
    ).toBe("`mydb`.`mytable`");
  });

  it("escapes special characters in database name", () => {
    expect(formatScopeSQL({ type: "database", database: "my-db" })).toBe("`my-db`.*");
  });

  it("escapes special characters in table name", () => {
    expect(
      formatScopeSQL({ type: "table", database: "my-db", table: "my.table" })
    ).toBe("`my-db`.`my.table`");
  });

  it("falls back to global when database is missing for database scope", () => {
    expect(formatScopeSQL({ type: "database" })).toBe("*.*");
  });

  it("falls back to database scope when table is missing for table scope", () => {
    expect(formatScopeSQL({ type: "table", database: "mydb" })).toBe("`mydb`.*");
  });

  it("falls back to global for unknown type", () => {
    expect(formatScopeSQL({ type: "unknown" })).toBe("*.*");
  });
});
