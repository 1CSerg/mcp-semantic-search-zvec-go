(class_definition
  name: (identifier) @name @scope.class) @boundary.class

(function_definition
  name: (identifier) @name @scope.function) @boundary.function

(decorated_definition) @boundary.decorated

(assignment) @boundary.assignment

(expression_statement) @boundary.expression
