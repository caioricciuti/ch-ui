import useAuthStore from "@/stores/user.store";


function ProfilePage() {
  const { user } = useAuthStore();

  return (
    <div>
      <h1>Profile</h1>
      <p>Name: {user?.name}</p>
      <p>Email: {user?.email}</p>
      {JSON.stringify(user)}
    </div>
  );
}

export default ProfilePage;