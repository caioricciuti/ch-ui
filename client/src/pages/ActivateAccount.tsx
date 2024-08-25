import { useEffect, useState } from "react";
import { Link, useParams, useNavigate } from "react-router-dom";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import * as z from "zod";
import api from "@/api/axios.config";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Loader2, LogIn, Mail } from "lucide-react";
import Logo from "/logo.png";
import { toast } from "sonner";

// Define the form schema with Zod
const formSchema = z.object({
  email: z.string().email({ message: "Invalid email address" }),
});

const ActivateAccount = () => {
  const { activationToken } = useParams<{ activationToken: string }>();
  const [status, setStatus] = useState<"loading" | "success" | "error">(
    "loading"
  );
  const [message, setMessage] = useState("");
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [resetTokenStatus, setResetTokenStatus] = useState<
    "idle" | "loading" | "success" | "error"
  >("idle");

  const navigate = useNavigate();

  // Initialize the form
  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      email: "",
    },
  });

  useEffect(() => {
    const confirmAccount = async () => {
      try {
        const response = await api.post("/auth/activate-account", {
          activationToken,
        });
        setStatus("success");
        setMessage(response.data.message);
      } catch (error: any) {
        setStatus("error");
        setMessage(
          error.response?.data?.message || "An unexpected error occurred"
        );
      }
    };

    confirmAccount();
  }, [activationToken]);

  const onSubmit = async (values: z.infer<typeof formSchema>) => {
    setResetTokenStatus("loading");
    try {
      const response = await api.post("/auth/reset-activation-token", values);
      setResetTokenStatus("success");
      // close the dialog and show toast
      setIsDialogOpen(false);
      // Navigate to login page
      navigate("/login");

      toast.success(response.data.message);
    } catch (error: any) {
      setResetTokenStatus("error");
      toast.error(
        error.response?.data?.message || "An unexpected error occurred"
      );
    }
  };

  return (
    <div className="flex items-center justify-center min-h-screen">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-2xl font-bold text-center flex items-center gap-4">
            <img src={Logo} alt="Logo" className="h-10 w-10" />
            Account Activation
          </CardTitle>
        </CardHeader>
        <CardContent>
          {status === "loading" && (
            <div className="flex flex-col items-center space-y-4">
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
              <p className="text-sm">Activating your account...</p>
            </div>
          )}
          {status === "success" && (
            <Alert variant="success">
              <AlertDescription>{message}</AlertDescription>
              <Button asChild className="mt-4 items-center flex gap-2">
                <Link to="/login">
                  <LogIn className="h-4 w-4" />
                  Login into your account
                </Link>
              </Button>
            </Alert>
          )}
          {status === "error" && (
            <Alert variant="destructive">
              <AlertTitle>Activation Failed</AlertTitle>
              <AlertDescription>{message}</AlertDescription>
              <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
                <DialogTrigger asChild>
                  <Button className="mt-4" variant="outline">
                    Request a new activation token
                  </Button>
                </DialogTrigger>
                <DialogContent className="sm:max-w-[425px]">
                  <DialogHeader>
                    <DialogTitle>Request New Activation Token</DialogTitle>
                    <DialogDescription>
                      Enter your email address to receive a new activation
                      token.
                    </DialogDescription>
                  </DialogHeader>
                  <Form {...form}>
                    <form
                      onSubmit={form.handleSubmit(onSubmit)}
                      className="space-y-8"
                    >
                      <FormField
                        control={form.control}
                        name="email"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>Email</FormLabel>
                            <FormControl>
                              <Input
                                placeholder="Enter your email"
                                {...field}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <DialogFooter>
                        <Button
                          type="submit"
                          disabled={resetTokenStatus === "loading"}
                        >
                          {resetTokenStatus === "loading" ? (
                            <Loader2 className="h-4 w-4 animate-spin mr-2" />
                          ) : (
                            <Mail className="h-4 w-4 mr-2" />
                          )}
                          Send New Token
                        </Button>
                      </DialogFooter>
                    </form>
                  </Form>
                </DialogContent>
              </Dialog>
            </Alert>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default ActivateAccount;
