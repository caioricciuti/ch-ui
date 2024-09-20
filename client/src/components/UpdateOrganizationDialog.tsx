// src/components/UpdateOrganizationDialog.tsx
import {useEffect} from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import useOrganizationStore from "@/stores/organization.store";
import { toast } from "sonner";

const formSchema = z.object({
  name: z.string().min(1, "Organization name is required"),
});

interface UpdateOrganizationDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const UpdateOrganizationDialog: React.FC<UpdateOrganizationDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const { selectedOrganization, updateOrganization } = useOrganizationStore();

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      name: selectedOrganization?.name || "",
    },
  });

  const onSubmit = async (values: z.infer<typeof formSchema>) => {
    if (selectedOrganization) {
      try {
        await updateOrganization(selectedOrganization._id, values.name);
        toast.success(`Organization ${values.name} updated successfully`);
      } catch (error) {
        if (error instanceof Error) {
          toast.error(error.message)
        } else {
          toast.error("Failed to update organization")
        }
      }
      onClose();
    }
  };

  useEffect(() => {
    if (selectedOrganization) {
      form.reset({ name: selectedOrganization.name });
    }
  }, [selectedOrganization, form]);

  if (!selectedOrganization) return null;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Update Organization</DialogTitle>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)}>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Organization Name</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter className="mt-4">
              <Button type="submit">Update Organization</Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default UpdateOrganizationDialog;
