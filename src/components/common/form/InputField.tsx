import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";

interface InputFieldProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  form: { Field: React.ComponentType<any> }
  name: string
  label?: string
  placeholder?: string
  validators?: Record<string, unknown>
}

function InputField({ form, name, label, placeholder, validators }: InputFieldProps) {
  return (
    <FormField
      form={form}
      name={name}
      validators={validators}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              placeholder={placeholder}
              value={field.state.value ?? ""}
              onChange={(e) => field.handleChange(e.target.value)}
              onBlur={field.handleBlur}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export default InputField;
