import React, { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
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
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";

const credentialSchema = z.object({
  name: z.string().min(1, "Name is required"),
  host: z.string().url("Host must be a valid URL"),
  port: z.coerce
    .number()
    .int()
    .min(1)
    .max(65535, "Port must be between 1 and 65535"),
  username: z.string().min(1, "Username is required"),
  password: z.string().optional(),
});

type CredentialFormValues = z.infer<typeof credentialSchema>;

interface UpdateCredentialDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const UpdateCredentialDialog: React.FC<UpdateCredentialDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const { selectedCredential, updateCredential } =
    useClickHouseCredentialStore();
  const [isTestingCredentials, setIsTestingCredentials] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isCredentialValid, setIsCredentialValid] = useState(false);

  const form = useForm<CredentialFormValues>({
    resolver: zodResolver(credentialSchema),
    defaultValues: {
      name: "",
      host: "https://",
      port: 8123,
      username: "",
      password: "",
    },
    mode: "onChange",
  });

  useEffect(() => {
    if (selectedCredential) {
      form.reset({
        name: selectedCredential.name,
        host: selectedCredential.host,
        port: selectedCredential.port,
        username: selectedCredential.username,
        password: "", // Don't populate the password for security reasons
      });
      setIsCredentialValid(true); // Assume the existing credential is valid
    }
  }, [selectedCredential, form]);

  const onSubmit = async (data: CredentialFormValues) => {
    if (!isCredentialValid) {
      toast.error("Please test and validate your credentials before updating.");
      return;
    }

    setIsSubmitting(true);
    try {
      if (selectedCredential) {
        await updateCredential(selectedCredential._id, {
          ...data,
          // Only include password if it's not empty
          ...(data.password && { password: data.password }),
        });
        toast.success("Credential updated successfully!");
        onClose();
        setIsCredentialValid(false);
      }
    } catch (error) {
      console.error("Failed to update credential:", error);
      toast.error("Failed to update credential. Please try again.");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleTestCredentials = async () => {
    setIsTestingCredentials(true);
    setIsCredentialValid(false);
    const data = form.getValues();

    try {
      const url = new URL(data.host);
      url.port = data.port.toString();
      const sqlQuery = "SELECT 1";

      const response = await fetch(
        `${url}?query=${encodeURIComponent(sqlQuery)}`,
        {
          method: "GET",
          headers: {
            "X-ClickHouse-User": data.username,
            "X-ClickHouse-Key": data.password || "",
          },
        }
      );

      if (response.ok) {
        toast.success("Credentials are valid!");
        setIsCredentialValid(true);
      } else {
        toast.error("Invalid credentials. Please check and try again.");
      }
    } catch (error) {
      console.error("Error testing credentials:", error);
      toast.error("An error occurred while testing credentials.");
    } finally {
      setIsTestingCredentials(false);
    }
  };

  const isFormValid = form.formState.isValid;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>Update ClickHouse Credential</DialogTitle>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Name</FormLabel>
                  <FormControl>
                    <Input {...field} placeholder="My ClickHouse Connection" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="host"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Host</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder="https://your-clickhouse-host.com"
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="port"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Port</FormLabel>
                  <FormControl>
                    <Input {...field} type="number" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Username</FormLabel>
                  <FormControl>
                    <Input {...field} placeholder="Your ClickHouse username" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Password</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type="password"
                      placeholder="Leave blank to keep current password"
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter className="gap-2">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={handleTestCredentials}
                      disabled={
                        !isFormValid || isTestingCredentials || isSubmitting
                      }
                    >
                      {isTestingCredentials ? (
                        <>
                          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                          Testing...
                        </>
                      ) : (
                        "Test Credentials"
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Verify your ClickHouse credentials before updating</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <Button
                type="submit"
                disabled={
                  !isFormValid ||
                  !isCredentialValid ||
                  isSubmitting ||
                  isTestingCredentials
                }
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Updating...
                  </>
                ) : (
                  "Update Credential"
                )}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default UpdateCredentialDialog;
