import * as React from "react"
import * as LabelPrimitive from "@radix-ui/react-label"
import { Slot } from "@radix-ui/react-slot"
import type { AnyFieldApi } from "@tanstack/react-form"

import { cn } from "@/lib/utils"
import { Label } from "@/components/ui/label"

// TanStack Form does not use a Provider pattern. Form is a passthrough wrapper
// for backwards compatibility so existing <Form> JSX does not break.
function Form({ children }: { children: React.ReactNode; [key: string]: unknown }) {
  return <>{children}</>
}
Form.displayName = "Form"

// --- FormField ---

type FormFieldContextValue = {
  fieldApi: AnyFieldApi
}

const FormFieldContext = React.createContext<FormFieldContextValue | null>(null)

// Structural type so the form prop accepts any TanStack React form instance
// (which extends FormApi with a `.Field` React component).
interface FormFieldProps {
  form: { Field: React.ComponentType<any> }
  name: string
  validators?: Record<string, unknown>
  render: (props: { field: AnyFieldApi }) => React.ReactNode
}

function FormField({ form, name, validators, render }: FormFieldProps) {
  return (
    <form.Field
      name={name}
      validators={validators}
      children={(field: AnyFieldApi) => (
        <FormFieldContext.Provider value={{ fieldApi: field }}>
          {render({ field })}
        </FormFieldContext.Provider>
      )}
    />
  )
}

// --- FormItem ---

type FormItemContextValue = {
  id: string
}

const FormItemContext = React.createContext<FormItemContextValue>(
  {} as FormItemContextValue
)

const FormItem = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => {
  const id = React.useId()

  return (
    <FormItemContext.Provider value={{ id }}>
      <div ref={ref} className={cn("space-y-2", className)} {...props} />
    </FormItemContext.Provider>
  )
})
FormItem.displayName = "FormItem"

// --- useFormField ---

function extractErrorMessage(
  errors: Array<unknown>
): string | undefined {
  if (errors.length === 0) return undefined

  const first = errors[0]
  if (typeof first === "string") return first
  if (
    first !== null &&
    typeof first === "object" &&
    "message" in first &&
    typeof (first as { message: unknown }).message === "string"
  ) {
    return (first as { message: string }).message
  }
  return String(first)
}

function useFormField() {
  const fieldContext = React.useContext(FormFieldContext)
  const itemContext = React.useContext(FormItemContext)

  if (!fieldContext) {
    throw new Error("useFormField should be used within <FormField>")
  }

  const { fieldApi } = fieldContext
  const { id } = itemContext

  return {
    id,
    name: fieldApi.name as string,
    formItemId: `${id}-form-item`,
    formDescriptionId: `${id}-form-item-description`,
    formMessageId: `${id}-form-item-message`,
    error: extractErrorMessage(fieldApi.state.meta.errors),
    isTouched: fieldApi.state.meta.isTouched,
    isDirty: fieldApi.state.meta.isDirty,
    isValidating: fieldApi.state.meta.isValidating,
  }
}

// --- FormLabel ---

const FormLabel = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>
>(({ className, ...props }, ref) => {
  const { error, formItemId } = useFormField()

  return (
    <Label
      ref={ref}
      className={cn(error && "text-destructive", className)}
      htmlFor={formItemId}
      {...props}
    />
  )
})
FormLabel.displayName = "FormLabel"

// --- FormControl ---

const FormControl = React.forwardRef<
  React.ElementRef<typeof Slot>,
  React.ComponentPropsWithoutRef<typeof Slot>
>(({ ...props }, ref) => {
  const { error, formItemId, formDescriptionId, formMessageId } = useFormField()

  return (
    <Slot
      ref={ref}
      id={formItemId}
      aria-describedby={
        !error
          ? `${formDescriptionId}`
          : `${formDescriptionId} ${formMessageId}`
      }
      aria-invalid={!!error}
      {...props}
    />
  )
})
FormControl.displayName = "FormControl"

// --- FormDescription ---

const FormDescription = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => {
  const { formDescriptionId } = useFormField()

  return (
    <p
      ref={ref}
      id={formDescriptionId}
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  )
})
FormDescription.displayName = "FormDescription"

// --- FormMessage ---

const FormMessage = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLParagraphElement>
>(({ className, children, ...props }, ref) => {
  const { error, formMessageId } = useFormField()
  const body = error ?? children

  if (!body) {
    return null
  }

  return (
    <p
      ref={ref}
      id={formMessageId}
      className={cn("text-sm font-medium text-destructive", className)}
      {...props}
    >
      {body}
    </p>
  )
})
FormMessage.displayName = "FormMessage"

export {
  Form,
  FormItem,
  FormLabel,
  FormControl,
  FormDescription,
  FormMessage,
  FormField,
  useFormField,
}
