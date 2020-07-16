package bot

import (
	"io/ioutil"
	"math/rand"
	"time"

	"github.com/anaskhan96/soup"
)

type Quiz struct {
	Questions []Question
}

type Question struct {
	Question string
	Choices  []string
	Correct  []int
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func (q Quiz) GetRandomQuestion() Question {
	s := rand.NewSource(time.Now().Unix())
	r := rand.New(s) // initialize local pseudorandom generator
	return q.Questions[r.Intn(len(q.Questions))]
}

func GetQuiz() Quiz {

	dat, err := ioutil.ReadFile("./exam1.html")
	check(err)

	doc := soup.HTMLParse(string(dat))
	Questions := doc.FindAll("div", "class", "panel-heading")
	FoundQuestions := make([]Question, len(Questions))

	for num, link := range Questions {
		titles := link.Find("div")
		if titles.Pointer != nil {
			FoundQuestions[num] = Question{Question: titles.Text()}
		}
	}

	Choices := doc.FindAll("ul", "class", "list-group")

	for num, answer := range Choices {
		choices := answer.FindAll("li", "class", "list-group-item")
		answers := []string{}
		correctAnswer := []int{}
		for choiceNum, choice := range choices {
			answers = append(answers, choice.Text())
			for _, children := range choice.Children() {
				if children.Attrs()["title"] == "Correct answer" {
					correctAnswer = append(correctAnswer, choiceNum)
				}
			}
		}
		FoundQuestions[num].Choices = answers
		FoundQuestions[num].Correct = correctAnswer
	}

	return Quiz{FoundQuestions}
}
