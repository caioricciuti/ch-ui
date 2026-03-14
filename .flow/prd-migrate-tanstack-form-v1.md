---
title: Migrate Forms from react-hook-form to TanStack React Form
status: approved
priority: P2
type: refactor
assignee: agent
created: 2026-02-24
tags: [forms, migration, tanstack, react-hook-form]
related_issues: []
---

# PRD: Migrate Forms to TanStack React Form

## Summary

Replace `react-hook-form` with `@tanstack/react-form` across the entire codebase. Rewrite the shared shadcn/ui form wrapper (`src/components/ui/form.tsx`) to use TanStack Form's API while preserving the same component names (FormField, FormItem, FormLabel, FormControl, FormMessage). Migrate all 4 forms and their sub-components, keeping Zod for validation (native Standard Schema support, no adapter needed).

## Motivation

- TanStack Form v1 offers native Zod support without adapter packages
- Better TypeScript inference from `defaultValues`
- First-class field-level async validation with debouncing
- `form.Subscribe` for granular re-render control
- `createFormHook` / `createFormHookContexts` for composable form libraries
- Controlled-only design eliminates uncontrolled input edge cases

## Scope

### Files to Modify

#### Core Form Infrastructure
1. `src/components/ui/form.tsx` - Rewrite shadcn form wrapper for TanStack Form
2. `src/components/common/form/InputField.tsx` - Update to use new form context

#### Connection Feature
3. `src/features/connections/components/ConnectionForm.tsx` - Migrate form
4. `src/features/connections/components/ConnectionForm.test.tsx` - Rewrite tests

#### Admin Feature (User Management)
5. `src/features/admin/components/CreateUser/index.tsx` - Migrate CreateNewUser form
6. `src/features/admin/components/CreateUser/EditUser.tsx` - Migrate EditUser form
7. `src/features/admin/components/CreateUser/AuthenticationSection.tsx` - Update field bindings
8. `src/features/admin/components/CreateUser/AccessControlSection.tsx` - Update field bindings
9. `src/features/admin/components/CreateUser/DatabaseRolesSection.tsx` - Update field bindings
10. `src/features/admin/components/CreateUser/SettingsSection.tsx` - Update field bindings

#### Admin Feature (Configuration)
11. `src/features/admin/components/ClickhouseDefaultConfiguration.tsx` - Migrate form

### Dependencies to Change

**Add:**
- `@tanstack/react-form` (pin exact version, e.g., `1.28.3`)

**Remove:**
- `react-hook-form`
- `@hookform/resolvers`

**Keep:**
- `zod` (used natively via Standard Schema)

## Technical Design

### 1. Form Wrapper (`src/components/ui/form.tsx`)

Replace the react-hook-form-based wrapper with TanStack Form equivalents. The key change is that `FormField` will no longer use `Controller` — instead it wraps `form.Field` and provides context for `FormItem`, `FormLabel`, `FormControl`, `FormMessage`.

**New architecture:**
- `FormFieldContext` stores the TanStack `FieldApi` instance (not just field name)
- `useFormField()` reads `FieldApi` from context to get errors, touched state, etc.
- `FormMessage` reads errors from `field.state.meta.errors` instead of `formState.errors`
- `FormControl` passes `field.handleChange`, `field.handleBlur`, and `field.state.value` via Slot

**API contract (preserved for consumers):**
```tsx
<FormField
  form={form}
  name="fieldName"
  validators={{ onChange: z.string().min(1) }}
  render={({ field }) => (
    <FormItem>
      <FormLabel>Label</FormLabel>
      <FormControl>
        <Input />
      </FormControl>
      <FormMessage />
    </FormItem>
  )}
/>
```

### 2. Validation Strategy

- **ConnectionForm**: Keep existing Zod schema, pass as `validators.onChange` at form level
- **Admin forms**: Convert inline `RegisterOptions` rules to Zod schemas or TanStack validator functions
- **Error display**: Handle both Standard Schema error objects (`.message`) and plain string errors

### 3. Migration Patterns

| react-hook-form | TanStack Form |
|---|---|
| `useForm({ resolver: zodResolver(schema) })` | `useForm({ validators: { onChange: schema } })` |
| `form.handleSubmit(onSubmit)` | `form.handleSubmit()` with `onSubmit` in useForm options |
| `form.watch("field")` | `useStore(form.store, s => s.values.field)` |
| `form.setValue("field", value)` | `form.setFieldValue("field", value)` |
| `<Controller>` | `<form.Field>` |
| `formState.isValid` | `form.state.canSubmit` |
| `formState.isSubmitting` | `form.state.isSubmitting` |
| `form.reset()` | `form.reset()` |

### 4. Test Migration

- Tests use `userEvent` and `screen` queries — these are UI-level and mostly unaffected
- Update form setup: replace `useForm` mocking with TanStack's `useForm`
- Update validation assertions if error message format changes
- Keep same test coverage and assertions

## Implementation Order

### Epic 1: Infrastructure Setup
- Install `@tanstack/react-form`
- Rewrite `src/components/ui/form.tsx`
- Update `src/components/common/form/InputField.tsx`

### Epic 2: ConnectionForm Migration
- Migrate `ConnectionForm.tsx`
- Rewrite `ConnectionForm.test.tsx`
- Verify connection create/edit/test flows work

### Epic 3: Admin Forms Migration
- Migrate `ClickhouseDefaultConfiguration.tsx`
- Migrate `CreateUser/index.tsx` (CreateNewUser)
- Migrate `CreateUser/EditUser.tsx`
- Update all section components (Authentication, AccessControl, DatabaseRoles, Settings)

### Epic 4: Cleanup
- Remove `react-hook-form` and `@hookform/resolvers` from dependencies
- Verify build succeeds with no react-hook-form references
- Run full test suite
- Verify TypeScript compilation

## Success Criteria

- [ ] All forms render and submit correctly
- [ ] Validation errors display properly
- [ ] ConnectionForm.test.tsx passes (rewritten for TanStack Form)
- [ ] No imports from `react-hook-form` remain in codebase
- [ ] `react-hook-form` and `@hookform/resolvers` removed from package.json
- [ ] TypeScript compilation succeeds
- [ ] Build succeeds (`bun run build`)
- [ ] No console errors in browser

## Risks

- **Standard Schema error format**: Zod errors via Standard Schema are objects with `.message`, not plain strings. `FormMessage` must handle both formats.
- **Form-level onChange re-render**: Using Zod schema as form-level `onChange` validator can cause excessive re-renders. Prefer field-level validators for `onChange` timing, form-level for `onSubmit`.
- **Controlled-only**: TanStack Form is controlled-only. All inputs must use `value` + `onChange`. This is already the pattern in this codebase.
