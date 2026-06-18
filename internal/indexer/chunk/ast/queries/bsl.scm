(procedure_definition
  name: (identifier) @name) @boundary.procedure

(function_definition
  name: (identifier) @name) @boundary.function

(var_definition) @boundary.var

(var_statement) @boundary.var

(string) @query_injection
