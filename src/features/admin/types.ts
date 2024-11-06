// types.ts
export interface UserData {
  name: string;
  id: string;
  auth_type: string[];
  host_ip?: string[];
  host_names?: string[];
  host_names_regexp?: string[];
  host_names_like?: string[];
  default_roles_all: number;
  default_roles_list?: string[];
  default_database?: string;
  grantees_any: number;
  grantees_list?: string[];
  grants?: any[];
  settings_profile?: string;
  readonly?: boolean;
}