// components/CreateNewUser/AuthenticationSection.tsx
import React, { useEffect, useState } from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Form, FormField, FormItem, FormLabel, FormControl, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Eye, EyeOff, Key, Check, X, Circle } from "lucide-react";

interface AuthenticationSectionProps {
  form: any;
  handleGeneratePassword: () => void;
}

const AuthenticationSection: React.FC<AuthenticationSectionProps> = ({ form, handleGeneratePassword }) => {
  const [showPassword, setShowPassword] = React.useState(false);
  const password = form.watch("password");

  const [reqs, setReqs] = useState({
    length: false,
    upper: false,
    lower: false,
    number: false,
    special: false
  });

  useEffect(() => {
    const val = password || "";
    setReqs({
      length: val.length >= 12,
      upper: /[A-Z]/.test(val),
      lower: /[a-z]/.test(val),
      number: /\d/.test(val),
      special: /[!@#$%^&*()_+\-=[\]{}|;:,.<>?]/.test(val)
    });
  }, [password]);

  const RequirementItem = ({ fulfilled, label }: { fulfilled: boolean; label: string }) => (
    <div className={`flex items-center gap-2 text-xs transition-colors duration-200 ${fulfilled ? 'text-green-400' : 'text-gray-500'}`}>
      <div className={`w-4 h-4 rounded-full flex items-center justify-center border ${fulfilled ? 'bg-green-500/10 border-green-500/50' : 'border-gray-700 bg-gray-800'}`}>
        {fulfilled ? <Check className="w-2.5 h-2.5" /> : <div className="w-1 h-1 rounded-full bg-gray-600" />}
      </div>
      <span>{label}</span>
    </div>
  );

  return (
    <div className="space-y-6">
      <FormField
        control={form.control}
        name="username"
        rules={{
          required: "Username is required",
          minLength: {
            value: 3,
            message: "Username must be at least 3 characters",
          },
          pattern: {
            value: /^[a-zA-Z0-9_]+$/,
            message: "Only letters, numbers, and underscores allowed",
          },
        }}
        render={({ field }) => (
          <FormItem>
            <FormLabel>Username</FormLabel>
            <FormControl>
              <Input placeholder="Enter username" {...field} className="bg-white/5 border-white/10 text-white" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="password"
        rules={{
          required: "Password is required",
          validate: (value: string) => {
            if (value.length < 12) return "Password must be at least 12 characters";
            if (!/[A-Z]/.test(value)) return "Must include uppercase letter";
            if (!/[a-z]/.test(value)) return "Must include lowercase letter";
            if (!/\d/.test(value)) return "Must include number";
            if (!/[!@#$%^&*()_+\-=[\]{}|;:,.<>?]/.test(value)) return "Must include special character";
            return true;
          },
        }}
        render={({ field }) => (
          <FormItem>
            <FormLabel>Password</FormLabel>
            <div className="flex space-x-2 w-full">
              <FormControl>
                <div className="relative flex-1">
                  <Input
                    type={showPassword ? "text" : "password"}
                    placeholder="Enter password"
                    className="w-full pr-10 bg-white/5 border-white/10 text-white font-mono"
                    {...field}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="absolute right-0 top-0 h-full px-3 text-gray-400 hover:text-white hover:bg-transparent"
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
                className="bg-white/5 border-white/10 text-white hover:bg-white/10"
              >
                <Key className="h-4 w-4 md:mr-2" />
                <span className="hidden md:block">Generate</span>
              </Button>
            </div>

            <div className="grid grid-cols-2 gap-2 mt-3 p-3 rounded-lg bg-black/20 border border-white/5">
              <RequirementItem fulfilled={reqs.length} label="Min. 12 Characters" />
              <RequirementItem fulfilled={reqs.upper} label="Uppercase Letter" />
              <RequirementItem fulfilled={reqs.lower} label="Lowercase Letter" />
              <RequirementItem fulfilled={reqs.number} label="Number" />
              <RequirementItem fulfilled={reqs.special} label="Special Character" />
            </div>

            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
};

export default AuthenticationSection;
