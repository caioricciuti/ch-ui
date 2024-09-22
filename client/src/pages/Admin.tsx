import { useEffect } from "react";
import useAuthStore from "@/stores/user.store";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

function AdminPage() {
  const { checkAuth, user, authIsLoading } = useAuthStore();
  const navigate = useNavigate();

  useEffect(() => {
    checkAuth();

    if (user?.role !== "admin") {
      navigate("/");
      toast.error("You are not authorized to view this page");
    }
  }, [navigate, checkAuth]);

  // Show loading state if the authentication check is still in progress
  if (authIsLoading) {
    return <div>Loading...</div>;
  }

  return (
    <div>
      <h1>Admin Page</h1>
    </div>
  );
}

export default AdminPage;
