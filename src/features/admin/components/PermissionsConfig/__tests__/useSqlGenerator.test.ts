import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useSqlGenerator } from "../hooks/useSqlGenerator";

// Mock useAppStore since it's not needed for SQL generation
vi.mock("@/store", () => ({
  default: () => ({}),
}));

describe("useSqlGenerator", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  function getGenerator() {
    const { result } = renderHook(() => useSqlGenerator());
    return result.current;
  }

  describe("generateCreateUser", () => {
    it("escapes username with backticks", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("admin", {
        password: "pass123",
      });
      expect(stmts[0]).toContain("CREATE USER `admin`");
    });

    it("escapes password with backslash escaping", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass'word",
      });
      expect(stmts[0]).toContain("BY 'pass\\'word'");
    });

    it("escapes username with special characters (backtick)", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user`name", {
        password: "pass",
      });
      expect(stmts[0]).toContain("CREATE USER `user``name`");
    });

    it("escapes default database", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
        defaultDatabase: "my-db",
      });
      expect(stmts[0]).toContain("DEFAULT DATABASE `my-db`");
    });

    it("escapes host IPs", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
        hostIp: ["192.168.1.1"],
      });
      expect(stmts[0]).toContain("HOST IP '192.168.1.1'");
    });

    it("escapes multiple host IPs", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
        hostIp: ["192.168.1.1", "10.0.0.1"],
      });
      expect(stmts[0]).toContain("HOST IP '192.168.1.1', '10.0.0.1'");
    });

    it("escapes host names", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
        hostNames: ["localhost", "example.com"],
      });
      expect(stmts[0]).toContain("HOST NAME 'localhost', 'example.com'");
    });

    it("defaults to HOST ANY when no host restrictions", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
      });
      expect(stmts[0]).toContain("HOST ANY");
    });

    it("escapes role names in GRANT statement", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
        defaultRoles: ["my-role"],
      });
      expect(stmts).toContainEqual(expect.stringContaining("GRANT `my-role` TO `user1`"));
      expect(stmts).toContainEqual(expect.stringContaining("SET DEFAULT ROLE `my-role` TO `user1`"));
    });

    it("escapes multiple role names", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("user1", {
        password: "pass",
        defaultRoles: ["role`1", "role-2"],
      });
      expect(stmts).toContainEqual(expect.stringContaining("GRANT `role``1`, `role-2` TO `user1`"));
    });

    it("generates complete CREATE USER statement with all options", () => {
      const gen = getGenerator();
      const stmts = gen.generateCreateUser("admin", {
        password: "secret",
        defaultDatabase: "analytics",
        hostIp: ["192.168.1.1"],
        defaultRoles: ["analyst"],
      });

      expect(stmts).toHaveLength(3);
      expect(stmts[0]).toContain("CREATE USER `admin`");
      expect(stmts[0]).toContain("IDENTIFIED WITH sha256_password BY 'secret'");
      expect(stmts[0]).toContain("HOST IP '192.168.1.1'");
      expect(stmts[0]).toContain("DEFAULT DATABASE `analytics`");
      expect(stmts[1]).toContain("GRANT `analyst` TO `admin`");
      expect(stmts[2]).toContain("SET DEFAULT ROLE `analyst` TO `admin`");
    });
  });

  describe("generateDropUser", () => {
    it("escapes username", () => {
      const gen = getGenerator();
      expect(gen.generateDropUser("admin")).toBe("DROP USER IF EXISTS `admin`");
    });

    it("prevents SQL injection in username", () => {
      const gen = getGenerator();
      const result = gen.generateDropUser("admin`; DROP TABLE users; --");
      expect(result).toBe("DROP USER IF EXISTS `admin``; DROP TABLE users; --`");
      // The malicious SQL is safely contained within backticks
      expect(result).toMatch(/^DROP USER IF EXISTS `[^`]*(?:``[^`]*)*`$/);
    });

    it("escapes username with backticks", () => {
      const gen = getGenerator();
      const result = gen.generateDropUser("user`name");
      expect(result).toBe("DROP USER IF EXISTS `user``name`");
    });
  });

  describe("generateCreateRole / generateDropRole", () => {
    it("escapes role name in CREATE", () => {
      const gen = getGenerator();
      expect(gen.generateCreateRole("analyst")).toBe("CREATE ROLE `analyst`");
    });

    it("escapes role name with special characters in CREATE", () => {
      const gen = getGenerator();
      expect(gen.generateCreateRole("role-name")).toBe("CREATE ROLE `role-name`");
    });

    it("escapes role name with backticks in CREATE", () => {
      const gen = getGenerator();
      expect(gen.generateCreateRole("role`name")).toBe("CREATE ROLE `role``name`");
    });

    it("escapes role name in DROP", () => {
      const gen = getGenerator();
      expect(gen.generateDropRole("analyst")).toBe("DROP ROLE IF EXISTS `analyst`");
    });

    it("prevents SQL injection in DROP", () => {
      const gen = getGenerator();
      const result = gen.generateDropRole("role`; DROP TABLE users; --");
      expect(result).toBe("DROP ROLE IF EXISTS `role``; DROP TABLE users; --`");
    });
  });

  describe("generateGrantRole / generateRevokeRole", () => {
    it("escapes both role and user in GRANT", () => {
      const gen = getGenerator();
      expect(gen.generateGrantRole("my-role", "my-user")).toBe(
        "GRANT `my-role` TO `my-user`"
      );
    });

    it("escapes role and user with backticks in GRANT", () => {
      const gen = getGenerator();
      expect(gen.generateGrantRole("role`name", "user`name")).toBe(
        "GRANT `role``name` TO `user``name`"
      );
    });

    it("escapes both role and user in REVOKE", () => {
      const gen = getGenerator();
      expect(gen.generateRevokeRole("my-role", "my-user")).toBe(
        "REVOKE `my-role` FROM `my-user`"
      );
    });

    it("prevents SQL injection in GRANT", () => {
      const gen = getGenerator();
      const result = gen.generateGrantRole("role`; DROP TABLE users; --", "user1");
      expect(result).toContain("`role``; DROP TABLE users; --`");
      // Verify that the role name is properly escaped within backticks
      expect(result).toMatch(/^GRANT `[^`]*(?:``[^`]*)*` TO `[^`]*(?:``[^`]*)*`$/);
    });
  });

  describe("generateCreateQuota", () => {
    it("escapes quota name", () => {
      const gen = getGenerator();
      const result = gen.generateCreateQuota("my-quota", { duration: "1 hour" });
      expect(result).toContain("CREATE QUOTA `my-quota`");
    });

    it("escapes quota name with backticks", () => {
      const gen = getGenerator();
      const result = gen.generateCreateQuota("quota`name", { duration: "1 hour" });
      expect(result).toContain("CREATE QUOTA `quota``name`");
    });

    it("generates quota with queries limit", () => {
      const gen = getGenerator();
      const result = gen.generateCreateQuota("my-quota", {
        duration: "1 hour",
        queries: 1000,
      });
      expect(result).toContain("QUERIES 1000");
    });

    it("generates quota with multiple constraints", () => {
      const gen = getGenerator();
      const result = gen.generateCreateQuota("my-quota", {
        duration: "1 day",
        queries: 1000,
        errors: 10,
        resultRows: 5000,
        readRows: 10000,
        executionTime: 3600,
      });
      expect(result).toContain("CREATE QUOTA `my-quota` FOR INTERVAL 1 day");
      expect(result).toContain("QUERIES 1000");
      expect(result).toContain("ERRORS 10");
      expect(result).toContain("RESULT ROWS 5000");
      expect(result).toContain("READ ROWS 10000");
      expect(result).toContain("EXECUTION TIME 3600");
    });

    it("prevents SQL injection in quota name", () => {
      const gen = getGenerator();
      const result = gen.generateCreateQuota("quota`; DROP TABLE users; --", { duration: "1 hour" });
      expect(result).toContain("`quota``; DROP TABLE users; --`");
    });
  });

  describe("generateDropQuota", () => {
    it("escapes quota name", () => {
      const gen = getGenerator();
      expect(gen.generateDropQuota("my-quota")).toBe("DROP QUOTA IF EXISTS `my-quota`");
    });

    it("prevents SQL injection", () => {
      const gen = getGenerator();
      const result = gen.generateDropQuota("quota`; DROP TABLE users; --");
      expect(result).toBe("DROP QUOTA IF EXISTS `quota``; DROP TABLE users; --`");
    });
  });

  describe("generateAlterUser", () => {
    it("escapes username and password", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", { password: "new'pass" });
      expect(stmts[0]).toContain("ALTER USER `user1`");
      expect(stmts[0]).toContain("BY 'new\\'pass'");
    });

    it("escapes default database", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", { defaultDatabase: "my-db" });
      expect(stmts[0]).toContain("ALTER USER `user1` DEFAULT DATABASE `my-db`");
    });

    it("escapes username with backticks", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user`name", { password: "pass" });
      expect(stmts[0]).toContain("ALTER USER `user``name`");
    });

    it("generates ALTER USER with host IP changes", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", {
        hostIp: ["192.168.1.1", "10.0.0.1"],
      });
      expect(stmts[0]).toContain("ALTER USER `user1` HOST IP '192.168.1.1', '10.0.0.1'");
    });

    it("generates ALTER USER with host name changes", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", {
        hostNames: ["localhost"],
      });
      expect(stmts[0]).toContain("ALTER USER `user1` HOST NAME 'localhost'");
    });

    it("generates multiple ALTER statements for different changes", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", {
        password: "newpass",
        defaultDatabase: "analytics",
        hostIp: ["192.168.1.1"],
      });

      expect(stmts).toHaveLength(3);
      expect(stmts[0]).toContain("ALTER USER `user1` IDENTIFIED WITH sha256_password BY 'newpass'");
      expect(stmts[1]).toContain("ALTER USER `user1` HOST IP '192.168.1.1'");
      expect(stmts[2]).toContain("ALTER USER `user1` DEFAULT DATABASE `analytics`");
    });

    it("escapes password with backslashes", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", { password: "pass\\word" });
      expect(stmts[0]).toContain("BY 'pass\\\\word'");
    });

    it("prevents SQL injection in database name", () => {
      const gen = getGenerator();
      const stmts = gen.generateAlterUser("user1", {
        defaultDatabase: "db`; DROP TABLE users; --",
      });
      expect(stmts[0]).toContain("DEFAULT DATABASE `db``; DROP TABLE users; --`");
    });
  });

  describe("generateGrant / generateRevoke", () => {
    const mockPermission = {
      id: "select",
      label: "SELECT",
      sqlPrivilege: "SELECT",
      children: [],
    };

    it("escapes username in GRANT", () => {
      const gen = getGenerator();
      const result = gen.generateGrant(
        mockPermission,
        { type: "global" },
        "user`name"
      );
      expect(result).toContain("`user``name`");
    });

    it("escapes username in REVOKE", () => {
      const gen = getGenerator();
      const result = gen.generateRevoke(
        mockPermission,
        { type: "global" },
        "user`name"
      );
      expect(result).toContain("FROM `user``name`");
    });

    it("escapes database name in scope", () => {
      const gen = getGenerator();
      const result = gen.generateRevoke(
        mockPermission,
        { type: "database", database: "db`name" },
        "user1"
      );
      expect(result).toContain("ON `db``name`.*");
    });

    it("escapes table name in scope", () => {
      const gen = getGenerator();
      const result = gen.generateRevoke(
        mockPermission,
        { type: "table", database: "db1", table: "table`name" },
        "user1"
      );
      expect(result).toContain("ON `db1`.`table``name`");
    });

    it("uses global scope for *.*", () => {
      const gen = getGenerator();
      const result = gen.generateRevoke(
        mockPermission,
        { type: "global" },
        "user1"
      );
      expect(result).toContain("ON *.*");
    });
  });
});
