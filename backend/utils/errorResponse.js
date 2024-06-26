const errorResponse = (
  res,
  statusCode,
  internalCode,
  message,
  scope,
  errors
) => {
  res.status(statusCode).json({
    code: statusCode,
    internalCode: internalCode,
    message: message,
    scope: scope,
    error: errors, 
  });
};

module.exports = errorResponse;
