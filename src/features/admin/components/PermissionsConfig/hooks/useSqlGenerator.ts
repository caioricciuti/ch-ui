import { useCallback } from "react";
import { generateGrantSql, PermissionNode, PermissionScope } from "../../CreateUser/PrivilegesSection/permissions";
import { escapeIdentifier, escapeStringLiteral, formatScopeSQL } from "@/features/admin/utils/sqlEscape";

/**
 * Hook for generating SQL statements for permission changes
 */
export function useSqlGenerator() {
  /**
   * Generate GRANT statement
   */
  const generateGrant = useCallback(
    (permission: PermissionNode, scope: PermissionScope, username: string): string => {
      return generateGrantSql(permission, scope, username);
    },
    []
  );

  /**
   * Generate REVOKE statement
   */
  const generateRevoke = useCallback(
    (permission: PermissionNode, scope: PermissionScope, username: string): string => {
      return `REVOKE ${permission.sqlPrivilege} ON ${formatScopeSQL(scope)} FROM ${escapeIdentifier(username)}`;
    },
    []
  );

  /**
   * Generate CREATE USER statement
   */
  const generateCreateUser = useCallback(
    (username: string, options: {
      password?: string;
      defaultDatabase?: string;
      defaultRoles?: string[];
      hostIp?: string[];
      hostNames?: string[];
    }): string[] => {
      const statements: string[] = [];

      // Build host restrictions
      let hostClause = '';
      if (options.hostIp && options.hostIp.length > 0) {
        const ips = options.hostIp.map(ip => `'${escapeStringLiteral(ip)}'`).join(', ');
        hostClause = ` HOST IP ${ips}`;
      } else if (options.hostNames && options.hostNames.length > 0) {
        const hosts = options.hostNames.map(h => `'${escapeStringLiteral(h)}'`).join(', ');
        hostClause = ` HOST NAME ${hosts}`;
      } else {
        hostClause = ' HOST ANY';
      }

      // Build CREATE USER statement
      let createStmt = `CREATE USER ${escapeIdentifier(username)}`;
      if (options.password) {
        createStmt += ` IDENTIFIED WITH sha256_password BY '${escapeStringLiteral(options.password)}'`;
      }
      createStmt += hostClause;

      if (options.defaultDatabase) {
        createStmt += ` DEFAULT DATABASE ${escapeIdentifier(options.defaultDatabase)}`;
      }

      statements.push(createStmt);

      // Add default roles if specified
      if (options.defaultRoles && options.defaultRoles.length > 0) {
        const roles = options.defaultRoles.map(r => escapeIdentifier(r)).join(', ');
        statements.push(`GRANT ${roles} TO ${escapeIdentifier(username)}`);
        statements.push(`SET DEFAULT ROLE ${roles} TO ${escapeIdentifier(username)}`);
      }

      return statements;
    },
    []
  );

  /**
   * Generate ALTER USER statement
   */
  const generateAlterUser = useCallback(
    (username: string, changes: {
      password?: string;
      defaultDatabase?: string;
      hostIp?: string[];
      hostNames?: string[];
    }): string[] => {
      const statements: string[] = [];

      if (changes.password) {
        statements.push(
          `ALTER USER ${escapeIdentifier(username)} IDENTIFIED WITH sha256_password BY '${escapeStringLiteral(changes.password)}'`
        );
      }

      if (changes.hostIp || changes.hostNames) {
        let hostClause = '';
        if (changes.hostIp && changes.hostIp.length > 0) {
          const ips = changes.hostIp.map(ip => `'${escapeStringLiteral(ip)}'`).join(', ');
          hostClause = `HOST IP ${ips}`;
        } else if (changes.hostNames && changes.hostNames.length > 0) {
          const hosts = changes.hostNames.map(h => `'${escapeStringLiteral(h)}'`).join(', ');
          hostClause = `HOST NAME ${hosts}`;
        } else {
          hostClause = 'HOST ANY';
        }
        statements.push(`ALTER USER ${escapeIdentifier(username)} ${hostClause}`);
      }

      if (changes.defaultDatabase !== undefined && changes.defaultDatabase !== "") {
        statements.push(
          `ALTER USER ${escapeIdentifier(username)} DEFAULT DATABASE ${escapeIdentifier(changes.defaultDatabase)}`
        );
      }

      return statements;
    },
    []
  );

  /**
   * Generate DROP USER statement
   */
  const generateDropUser = useCallback((username: string): string => {
    return `DROP USER IF EXISTS ${escapeIdentifier(username)}`;
  }, []);

  /**
   * Generate CREATE ROLE statement
   */
  const generateCreateRole = useCallback((roleName: string): string => {
    return `CREATE ROLE ${escapeIdentifier(roleName)}`;
  }, []);

  /**
   * Generate DROP ROLE statement
   */
  const generateDropRole = useCallback((roleName: string): string => {
    return `DROP ROLE IF EXISTS ${escapeIdentifier(roleName)}`;
  }, []);

  /**
   * Generate GRANT ROLE statement
   */
  const generateGrantRole = useCallback((roleName: string, username: string): string => {
    return `GRANT ${escapeIdentifier(roleName)} TO ${escapeIdentifier(username)}`;
  }, []);

  /**
   * Generate REVOKE ROLE statement
   */
  const generateRevokeRole = useCallback((roleName: string, username: string): string => {
    return `REVOKE ${escapeIdentifier(roleName)} FROM ${escapeIdentifier(username)}`;
  }, []);

  /**
   * Generate CREATE QUOTA statement
   */
  const generateCreateQuota = useCallback(
    (quotaName: string, options: {
      duration: string;
      queries?: number;
      errors?: number;
      resultRows?: number;
      readRows?: number;
      executionTime?: number;
    }): string => {
      let stmt = `CREATE QUOTA ${escapeIdentifier(quotaName)} FOR INTERVAL ${options.duration}`;

      if (options.queries !== undefined) {
        stmt += ` QUERIES ${options.queries}`;
      }
      if (options.errors !== undefined) {
        stmt += ` ERRORS ${options.errors}`;
      }
      if (options.resultRows !== undefined) {
        stmt += ` RESULT ROWS ${options.resultRows}`;
      }
      if (options.readRows !== undefined) {
        stmt += ` READ ROWS ${options.readRows}`;
      }
      if (options.executionTime !== undefined) {
        stmt += ` EXECUTION TIME ${options.executionTime}`;
      }

      return stmt;
    },
    []
  );

  /**
   * Generate DROP QUOTA statement
   */
  const generateDropQuota = useCallback((quotaName: string): string => {
    return `DROP QUOTA IF EXISTS ${escapeIdentifier(quotaName)}`;
  }, []);

  return {
    generateGrant,
    generateRevoke,
    generateCreateUser,
    generateAlterUser,
    generateDropUser,
    generateCreateRole,
    generateDropRole,
    generateGrantRole,
    generateRevokeRole,
    generateCreateQuota,
    generateDropQuota,
  };
}

/**
 * Format scope for SQL
 */
function formatScope(scope: PermissionScope): string {
  return formatScopeSQL(scope);
}
