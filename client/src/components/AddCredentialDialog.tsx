import React, { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import { toast } from "sonner";
import { Loader2, Check } from "lucide-react";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import useClickHouseCredentialStore from "@/stores/clickHouseCredentials.store";
import useOrganizationStore from "@/stores/organization.store";

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
  allowedOrganizations: z
    .array(z.string())
    .min(1, "At least one organization is required"),
});

type CredentialFormValues = z.infer<typeof credentialSchema>;

interface AddCredentialDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const AddCredentialDialog: React.FC<AddCredentialDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const { createCredential, fetchAvailableCredentials, fetchCredentials } =
    useClickHouseCredentialStore();
  const { organizations, fetchOrganizations } = useOrganizationStore();
  const [isTestingCredentials, setIsTestingCredentials] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isCredentialValid, setIsCredentialValid] = useState(false);

  useEffect(() => {
    fetchOrganizations();
  }, [fetchOrganizations]);

  const form = useForm<CredentialFormValues>({
    resolver: zodResolver(credentialSchema),
    defaultValues: {
      name: "",
      host: "https://",
      port: 8123,
      username: "",
      password: "",
      allowedOrganizations: [],
    },
    mode: "onChange",
  });

  const onSubmit = async (data: CredentialFormValues) => {
    if (!isCredentialValid) {
      toast.error("Please test and validate your credentials before adding.");
      return;
    }

    setIsSubmitting(true);
    try {
      await createCredential(data);
      toast.success("Credential added successfully!");
      onClose();
      form.reset();
      setIsCredentialValid(false);
      fetchCredentials();
      fetchAvailableCredentials();
    } catch (error) {
      console.error("Failed to create credential:", error);
      toast.error("Failed to add credential. Please try again.");
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
          <DialogTitle>Add New ClickHouse Credential</DialogTitle>
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
                      placeholder="Your ClickHouse password"
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="allowedOrganizations"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Allowed Organizations</FormLabel>
                  <Select
                    onValueChange={(value) => {
                      const updatedOrgs = field.value.includes(value)
                        ? field.value.filter((org) => org !== value)
                        : [...field.value, value];
                      field.onChange(updatedOrgs);
                    }}
                    value={field.value[field.value.length - 1] || ""}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select organizations" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {organizations.map((org) => (
                        <SelectItem key={org._id} value={org._id}>
                          <div className="flex items-center">
                            <span>{org.name}</span>
                            {field.value.includes(org._id) && (
                              <Check className="ml-2 h-4 w-4" />
                            )}
                          </div>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <div className="flex flex-wrap gap-2 mt-2">
                    {field.value.map((orgId) => {
                      const org = organizations.find((o) => o._id === orgId);
                      return (
                        <Badge
                          key={orgId}
                          variant="secondary"
                          className="cursor-pointer"
                          onClick={() => {
                            const updatedOrgs = field.value.filter(
                              (id) => id !== orgId
                            );
                            field.onChange(updatedOrgs);
                          }}
                        >
                          {org?.name}
                          <span className="ml-1">×</span>
                        </Badge>
                      );
                    })}
                  </div>
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
                    <p>Verify your ClickHouse credentials before adding</p>
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
                    Adding...
                  </>
                ) : (
                  "Add Credential"
                )}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default AddCredentialDialog;
