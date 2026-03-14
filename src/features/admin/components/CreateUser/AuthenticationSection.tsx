import React from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Eye, EyeOff, Key } from "lucide-react";

interface AuthenticationSectionProps {
  form: any;
  handleGeneratePassword: () => void;
  isEditMode?: boolean;
}

const AuthenticationSection: React.FC<AuthenticationSectionProps> = ({
  form,
  handleGeneratePassword,
  isEditMode = false,
}) => {
  const [showPassword, setShowPassword] = React.useState(false);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Authentication</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormField
          form={form}
          name="username"
          validators={{
            onChange: ({ value }: { value: string }) => {
              if (!value) return "Username is required";
              if (value.length < 3)
                return "Username must be at least 3 characters";
              if (!/^[a-zA-Z0-9_]+$/.test(value))
                return "Only letters, numbers, and underscores allowed";
              return undefined;
            },
          }}
          render={({ field }) => (
            <FormItem>
              <FormLabel>Username</FormLabel>
              <FormControl>
                <Input
                  placeholder="Enter username"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  readOnly={isEditMode}
                  disabled={isEditMode}
                  className={isEditMode ? "bg-muted disabled:opacity-100 text-muted-foreground" : ""}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          form={form}
          name="password"
          validators={{
            onChange: ({ value }: { value: string }) => {
              if (isEditMode && !value) return undefined;
              if (!isEditMode && !value) return "Password is required";
              if (value && value.length < 8)
                return "Password must be at least 8 characters";
              return undefined;
            },
          }}
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {isEditMode ? "New Password (Optional)" : "Password"}
              </FormLabel>
              <div className="flex space-x-2 w-full">
                <FormControl>
                  <div className="relative flex-1">
                    <Input
                      type={showPassword ? "text" : "password"}
                      placeholder={
                        isEditMode
                          ? "Leave blank to keep current password"
                          : "Enter password"
                      }
                      className="w-full"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onBlur={field.handleBlur}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="absolute right-2 top-0 h-full px-3"
                      onClick={() => setShowPassword(!showPassword)}
                    >
                      {showPassword ? (
                        <EyeOff className="h-4 w-4" />
                      ) : (
                        <Eye className="h-4 w-4" />
                      )}
                    </Button>
                  </div>
                </FormControl>
                <Button
                  type="button"
                  onClick={handleGeneratePassword}
                  variant="outline"
                >
                  <Key className="h-4 w-4 md:mr-2" />
                  <span className="hidden md:block">Generate</span>
                </Button>
              </div>
              {isEditMode && (
                <p className="text-sm text-muted-foreground">
                  Leave blank to keep current password
                </p>
              )}
              <FormMessage />
            </FormItem>
          )}
        />
      </CardContent>
    </Card>
  );
};

export default AuthenticationSection;
