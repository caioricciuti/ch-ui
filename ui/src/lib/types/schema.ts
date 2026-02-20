/** Database in the schema tree */
export interface Database {
  name: string
  tables?: Table[]
  expanded?: boolean
  loading?: boolean
}

/** Table in the schema tree */
export interface Table {
  name: string
  columns?: Column[]
  expanded?: boolean
  loading?: boolean
}

/** Column in the schema tree */
export interface Column {
  name: string
  type: string
  default_type?: string
  default_expression?: string
  comment?: string
}
