// AI-generated snippet: XSS via innerHTML with unsanitized user input.
const name = userInput;
document.getElementById("greeting").innerHTML = "<p>Hello " + name + "</p>";
