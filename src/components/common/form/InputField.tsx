import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { HTMLProps } from "react";
import { FieldValues, RegisterOptions, useFormContext } from "react-hook-form";


interface Props<T extends FieldValues> extends HTMLProps<HTMLInputElement> {
  rules?: Omit<RegisterOptions<FieldValues, string>, "disabled" | "valueAsNumber" | "valueAsDate" | "setValueAs"> | undefined,

}

const InputField = <T extends FieldValues>({ name, rules, label, placeholder }: Props<T>) => {
  const context = useFormContext()
  return (
    <FormField control={context.control}
      name={name as string}
      rules={rules}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input placeholder={placeholder} {...field} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}>
    </FormField>
  )
}

export default InputField;