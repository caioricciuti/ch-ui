// helper to get user initials

export const getInitials = (name: string) => {
  const names = name.split(" ");

  if (names.length === 1) {
    // If there is only one name, return the first two letters
    return names[0].slice(0, 2).toUpperCase();
  } else {
    // Get the first letter of the first and last name
    const firstName = names[0];
    const lastName = names[names.length - 1];
    return `${firstName.charAt(0).toUpperCase()}${lastName
      .charAt(0)
      .toUpperCase()}`;
  }
};

export const bgColorsByInitials = (initials: string) => {
  const charCode = initials.charCodeAt(0) + initials.charCodeAt(1);
  const colors = [
    "bg-blue-500",
    "bg-green-500",
    "bg-yellow-500",
    "bg-indigo-500",
    "bg-pink-500",
    "bg-purple-500",
    "bg-red-500",
    "bg-teal-500",
    "bg-gray-500",
    "bg-orange-500",
  ];
  return colors[charCode % colors.length];
};

export const bgGradientByInitials = (initials: string) => {
  const charCode = initials.charCodeAt(0) + initials.charCodeAt(1);
  const gradients = [
    "bg-gradient-to-r from-blue-600 via-green-500 to-indigo-400",
    "bg-gradient-to-r from-purple-600 via-pink-500 to-red-400",
    "bg-gradient-to-r from-yellow-600 via-orange-500 to-red-400",
    "bg-gradient-to-r from-green-600 via-teal-500 to-blue-400",
    "bg-gradient-to-r from-pink-600 via-purple-500 to-indigo-400",
    "bg-gradient-to-r from-indigo-600 via-blue-500 to-green-400",
    "bg-gradient-to-r from-red-600 via-orange-500 to-yellow-400",
    "bg-gradient-to-r from-teal-600 via-blue-500 to-indigo-400",
    "bg-gradient-to-r from-gray-600 via-gray-500 to-gray-400",
    "bg-gradient-to-r from-orange-600 via-yellow-500 to-green-400",
  ];
  return gradients[charCode % gradients.length];
};